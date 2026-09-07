"""The import chain.

    chain(ingest_paper.s(pmid) | embed_paper.s())

Two stages, split where the failure economics differ. `ingest_paper` is bound
by NCBI's ~3 req/s and costs another rate-limited fetch to retry; `embed_paper`
is CPU-bound and free to retry locally. Chunking rides along with ingest
because chunks are 1:1 with PubTator passages — a list comprehension over data
already in hand, which would not earn its own broker hop.

Tasks pass a `paper_id`, never a payload. The fetched document is ~130KB and
the vectors for one paper are larger still; both stay in Postgres and only an
integer crosses the broker.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Optional

from celery import chain

from api.app.config import get_settings
from api.cache import DocumentCache
from api.ingestion.celery_app import celery_app
from api.db import session_scope
from api.db.models import PaperChunk
from api.ingestion import persist
from api.eu_client import naming
from api.ingestion.embedding import embed_texts, embedding_fingerprint
from api.ncbi import http as ncbi_http
from api.redis_conn import close_client as close_redis
from api.pb_client.models import PaperResponse
from api.pb_client.pubtator import PubTatorClient

log = logging.getLogger(__name__)

#: Bumped when the ingest mapping changes in a way that should re-run the stage.
INGEST_VERSION = "1"

#: Postgres class 40 — serialization_failure and deadlock_detected. The
#: transaction was rolled back for a reason that is not this paper's fault and
#: will usually not recur, so the victim should try again rather than die.
#: `persist` sorts its inserts to make deadlocks unlikely; this handles the
#: residue, and anything else two concurrent writers can still collide on.
TRANSIENT_SQLSTATES = frozenset({"40001", "40P01"})


def is_transient_conflict(exc: BaseException) -> bool:
    """True for a rollback that retrying can fix."""
    return getattr(getattr(exc, "orig", None), "sqlstate", None) in TRANSIENT_SQLSTATES


async def _fetch(pmid: int) -> tuple[PaperResponse, dict]:
    """Fetch one paper, returning (normalised dict, raw upstream document).

    The httpx client is built here, inside `asyncio.run`, rather than cached on
    the module. That costs one TLS handshake per paper — irrelevant beside the
    3 req/s limit — and avoids holding an AsyncClient bound to an event loop
    that `asyncio.run` has already closed.

    The document cache is consulted first. `/corpus/{id}/references` fetches
    exactly these documents to decide what is importable, so an import started
    from the reader usually costs no PubTator request at all. A miss, or a
    cache that is down, simply fetches — which is what keeps ingest working
    when Redis does not.
    """
    settings = get_settings()
    cache = DocumentCache(
        settings.redis_cache_url,
        ttl_seconds=settings.document_cache_ttl_seconds,
        timeout=settings.document_cache_timeout_seconds,
        enabled=settings.document_cache_enabled,
    )
    try:
        async with ncbi_http.build_client(
            timeout=settings.http_timeout_seconds,
            contact_email=settings.ncbi_contact_email,
            limiter=ncbi_http.RedisRateLimiter(
                settings.rate_limit_redis_url, settings.ncbi_rate_limit_per_second
            ),
        ) as client:
            pubtator = PubTatorClient(client, settings.pubtator_base_url, cache=cache)
            # One request, both representations — asking twice would double our
            # load on a service that tolerates ~3 requests/second.
            paper, raw = await pubtator.fetch_paper_with_raw(pmid, full=True)
    finally:
        # This loop dies with the task; neither connection pool should outlive it.
        await cache.aclose()
        await close_redis(settings.rate_limit_redis_url)
    return paper, raw


def _resolve_names(paper: PaperResponse) -> dict[str, str]:
    """Names for the concepts PubTator left as bare identifiers.

    Runs between the fetch and the write, on the paper in memory, so the names
    go in with the insert. Two reasons that matters: nothing then has to
    overwrite `name` on rows every other paper also touches, and a lookup that
    fails leaves the identifier standing rather than a value someone has to
    correct later.

    Fail-open and outside any transaction. A missing label is cosmetic, and the
    backfill repairs whatever this could not reach.
    """
    concepts = persist.unnamed_concepts(paper)
    if not concepts:
        return {}
    try:
        with session_scope() as session:
            known = naming.already_named(session, list(concepts))
        pending = {i: db for i, db in concepts.items() if i not in known}
        if not pending:
            return {}
        names = asyncio.run(naming.resolve_identifiers(pending))
        if names:
            log.info("resolved %d name(s) for PMID %s", len(names), paper.pmid)
        return names
    except Exception:
        log.warning("could not resolve names for PMID %s", paper.pmid, exc_info=True)
        return {}


async def _forget(pmid: int) -> None:
    """Drop a cached document. Best effort: it expires on its own regardless."""
    settings = get_settings()
    cache = DocumentCache(
        settings.redis_cache_url,
        ttl_seconds=settings.document_cache_ttl_seconds,
        timeout=settings.document_cache_timeout_seconds,
        enabled=settings.document_cache_enabled,
    )
    try:
        await cache.delete(pmid)
    finally:
        await cache.aclose()


@celery_app.task(bind=True, name="api.ingestion.tasks.ingest_paper", max_retries=3)
def ingest_paper(self, pmid: int, force: bool = False) -> int:
    """Fetch a paper from PubTator and write it, chunked, to Postgres.

    Calls `PubTatorClient` directly rather than our own `/pb/paper` endpoint:
    same code path, no extra hop, and the worker does not depend on the web
    process being up.

    Returns the `papers.id` for the next stage.
    """
    with session_scope() as session:
        paper_id = persist.reserve_paper(session, pmid)
        if not force and persist.stage_is_current(session, paper_id, "ingest", INGEST_VERSION):
            log.info("ingest for PMID %s already current, skipping", pmid)
            return paper_id
        persist.mark_stage(session, paper_id, "ingest", "running")

    try:
        paper, raw = asyncio.run(_fetch(pmid))
    except Exception as exc:
        with session_scope() as session:
            persist.mark_stage(session, paper_id, "ingest", "failed", error=str(exc)[:500])
        log.exception("ingest fetch failed for PMID %s", pmid)
        raise self.retry(exc=exc, countdown=30) from exc

    # Before the transaction opens, so no lock is held across a network call.
    names = _resolve_names(paper)

    try:
        with session_scope() as session:
            _, chunk_count = persist.persist_paper(session, paper, raw, names)
            persist.mark_stage(
                session, paper_id, "ingest", "done", fingerprint=INGEST_VERSION
            )
        # Postgres owns the document now, so holding a 24h copy in a capped
        # cache would spend the budget on the one paper that no longer needs it.
        asyncio.run(_forget(pmid))
    except Exception as exc:
        # A deadlock is not a failed import, it is a lost race. Leave the row
        # pending -- which reads as "queued" to the client -- so a retry is not
        # reported to the user as an error it is not.
        retryable = is_transient_conflict(exc) and self.request.retries < self.max_retries
        with session_scope() as session:
            persist.mark_stage(
                session,
                paper_id,
                "ingest",
                "pending" if retryable else "failed",
                error=str(exc)[:500],
            )
        if retryable:
            log.warning(
                "ingest for PMID %s hit a transient conflict, retrying: %s",
                pmid,
                str(exc)[:200],
            )
            raise self.retry(exc=exc, countdown=5) from exc
        raise

    log.info("ingested PMID %s as paper %s (%d chunks)", pmid, paper_id, chunk_count)
    return paper_id


@celery_app.task(bind=True, name="api.ingestion.tasks.embed_paper", max_retries=2)
def embed_paper(self, paper_id: int, force: bool = False) -> int:
    """Fill `paper_chunks.embedding` for one paper. Returns chunks embedded.

    Separate from ingest because it is the slow half and must be retryable —
    and re-runnable after a model change — without another PubTator fetch.
    """
    fingerprint = embedding_fingerprint()

    with session_scope() as session:
        if not force and persist.stage_is_current(session, paper_id, "embed", fingerprint):
            log.info("embeddings for paper %s already current, skipping", paper_id)
            return 0
        persist.mark_stage(session, paper_id, "embed", "running")
        rows = session.execute(
            PaperChunk.__table__.select()
            .with_only_columns(PaperChunk.id, PaperChunk.text)
            .where(PaperChunk.paper_id == paper_id)
            .order_by(PaperChunk.ordinal)
        ).all()

    if not rows:
        with session_scope() as session:
            persist.mark_stage(
                session, paper_id, "embed", "done", fingerprint=fingerprint
            )
        log.info("paper %s has no chunks to embed", paper_id)
        return 0

    try:
        vectors = embed_texts([text for _, text in rows])
        with session_scope() as session:
            for (chunk_id, _), vector in zip(rows, vectors):
                session.execute(
                    PaperChunk.__table__.update()
                    .where(PaperChunk.id == chunk_id)
                    .values(embedding=vector)
                )
            persist.mark_stage(
                session, paper_id, "embed", "done", fingerprint=fingerprint
            )
    except Exception as exc:
        with session_scope() as session:
            persist.mark_stage(session, paper_id, "embed", "failed", error=str(exc)[:500])
        log.exception("embedding failed for paper %s", paper_id)
        raise self.retry(exc=exc, countdown=10) from exc

    log.info("embedded %d chunks for paper %s", len(rows), paper_id)
    return len(rows)


def import_paper(pmid: int, *, force: bool = False) -> Optional[object]:
    """Queue the full chain for one PMID. Returns the chain's AsyncResult.

    `app=celery_app` is not decoration. Celery resolves the current app from
    thread-local state, and FastAPI runs sync routes in a threadpool, so a bare
    `chain(...)` off the main thread silently falls back to Celery's *default*
    app — which points at RabbitMQ on 5672 and fails with a bare
    "Connection refused". Naming the app makes the resolution explicit; the
    tasks are bound to it directly for the same reason.
    """
    return chain(
        ingest_paper.s(pmid, force=force), embed_paper.s(force=force), app=celery_app
    ).apply_async()
