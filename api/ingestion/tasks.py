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
from api.ingestion.celery_app import celery_app
from api.db import session_scope
from api.db.models import PaperChunk
from api.ingestion import persist
from api.ingestion.embedding import embed_texts, embedding_fingerprint
from api.pb_client import http as pb_http
from api.pb_client.models import PaperResponse
from api.pb_client.pubtator import PubTatorClient

log = logging.getLogger(__name__)

#: Bumped when the ingest mapping changes in a way that should re-run the stage.
INGEST_VERSION = "1"


async def _fetch(pmid: int) -> tuple[PaperResponse, dict]:
    """Fetch one paper, returning (normalised dict, raw upstream document).

    The httpx client is built here, inside `asyncio.run`, rather than cached on
    the module. That costs one TLS handshake per paper — irrelevant beside the
    3 req/s limit — and avoids holding an AsyncClient bound to an event loop
    that `asyncio.run` has already closed.
    """
    settings = get_settings()
    async with pb_http.build_client(
        timeout=settings.http_timeout_seconds, contact_email=settings.ncbi_contact_email
    ) as client:
        pubtator = PubTatorClient(client, settings.pubtator_base_url)
        # One request, both representations — asking twice would double our
        # load on a service that tolerates ~3 requests/second.
        paper, raw = await pubtator.fetch_paper_with_raw(pmid, full=True)
    return paper, raw


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

    try:
        with session_scope() as session:
            _, chunk_count = persist.persist_paper(session, paper, raw)
            persist.mark_stage(
                session, paper_id, "ingest", "done", fingerprint=INGEST_VERSION
            )
    except Exception as exc:
        with session_scope() as session:
            persist.mark_stage(session, paper_id, "ingest", "failed", error=str(exc)[:500])
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
