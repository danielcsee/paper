"""The import chain.

    chain(fetch_paper.s(pmid) | persist_paper.s() | embed_chunks.s())

Tasks pass a `paper_id`, never a payload. A PubTator document is ~130KB; moving
it through the broker would make every message huge and every stage dependent
on the previous one's return value. Writing it to `paper_pubtator_docs` in
stage one instead keeps messages to a single integer and lets any later stage
re-run from stored state.

Stage boundaries follow `paper_stage_runs`, so a failure resumes from the
failed stage rather than from the rate-limited fetch.
"""

from __future__ import annotations

from celery import shared_task


@shared_task(bind=True, name="api.ingestion.tasks.fetch_paper", max_retries=3)
def fetch_paper(self, pmid: int) -> int:
    """Fetch one paper from PubTator and store the raw response.

    Calls `PubTatorClient.fetch_paper` directly rather than our own HTTP
    endpoint: same code path, no extra hop, and the worker does not depend on
    the web process being up.

    The client is built inside `asyncio.run` per task. That costs one TLS
    handshake per paper, which is irrelevant next to the 3 req/s rate limit,
    and it avoids caching an AsyncClient across event loops — a closed-loop bug
    waiting to happen.

    Returns the `papers.id` for the rest of the chain.
    """
    raise NotImplementedError


@shared_task(bind=True, name="api.ingestion.tasks.persist_paper")
def persist_paper(self, paper_id: int) -> int:
    """Normalise the stored response into rows.

    Authors, references, entities, mentions, relations, chunks and body_text.
    Chunks are written without vectors; embedding is a separate stage because
    it is the slow part and must be retryable without re-fetching.
    """
    raise NotImplementedError


@shared_task(bind=True, name="api.ingestion.tasks.embed_chunks")
def embed_chunks(self, paper_id: int) -> int:
    """Fill `paper_chunks.embedding` for one paper. Returns chunks embedded."""
    raise NotImplementedError


def import_paper(pmid: int, *, force: bool = False):
    """Queue the full chain for one PMID. Returns the AsyncResult."""
    raise NotImplementedError
