"""HTTP surface for the ingestion pipeline, mounted at /import.

Both routes are sync (`def`, not `async def`) on purpose. They talk to Postgres
through SQLAlchemy's blocking API, so FastAPI runs them in its threadpool; an
`async def` would block the event loop for the whole query.
"""

from __future__ import annotations

import logging
from collections import Counter

from fastapi import APIRouter, HTTPException, Query

from api.db import session_scope
from api.ingestion import persist
from api.ingestion.models import (
    MAX_BATCH,
    ImportJob,
    ImportRequest,
    ImportResponse,
    ImportStatusResponse,
)
from api.ingestion.tasks import import_paper

log = logging.getLogger(__name__)
router = APIRouter(tags=["import"])


@router.post("/import", response_model=ImportResponse, status_code=202, summary="Queue papers")
def import_papers(request: ImportRequest) -> ImportResponse:
    """Kick off one chain per selected paper.

    Returns immediately with a job per paper, in request order. Three outcomes:

    * **already_imported** — the paper's final stage is `done`.
    * **in_progress** — an earlier chain is still working on it. Nothing is
      queued: two chains on the same paper would write the same rows
      concurrently, and Celery does not deduplicate.
    * **queued** — a chain was submitted; `task_id` is the chain's id.

    `force: true` overrides both ledger states, including `in_progress` — the
    escape hatch for a chain that died without marking itself failed.

    A PMID repeated inside one request is queued once.
    """
    pmids: list[int] = [item.pmid for item in request.pmids]

    states: dict[int, persist.LedgerState] = {}
    if pmids and not request.force:
        with session_scope() as session:
            states = persist.import_states(session, pmids)

    jobs: list[ImportJob] = []
    queued: dict[int, str] = {}

    for item in request.pmids:
        state = states.get(item.pmid)
        if state == "complete":
            jobs.append(ImportJob(pmid=item.pmid, status="already_imported"))
            continue
        if state == "in_progress":
            jobs.append(
                ImportJob(
                    pmid=item.pmid,
                    status="in_progress",
                    reason="an earlier import is still running; pass force to re-queue",
                )
            )
            continue

        # Same PMID twice in one selection: report both, queue one.
        if item.pmid in queued:
            jobs.append(ImportJob(pmid=item.pmid, status="queued", task_id=queued[item.pmid]))
            continue

        try:
            # Reserve the row and mark ingest pending *before* queueing, so a
            # paper is visible as in_progress from the moment it is requested
            # rather than only once its first task finishes.
            with session_scope() as session:
                reserved_id = persist.reserve_paper(session, item.pmid)
                persist.mark_queued(session, reserved_id, ("ingest", "embed", "graph"))
            result = import_paper(item.pmid, force=request.force)
        except Exception as exc:  # broker unreachable, mainly
            log.exception("could not queue PMID %s", item.pmid)
            raise HTTPException(
                status_code=503, detail=f"could not queue import: {exc}"
            ) from exc

        task_id = getattr(result, "id", None)
        if task_id is not None:
            queued[item.pmid] = task_id
        jobs.append(ImportJob(pmid=item.pmid, status="queued", task_id=task_id))

    counts = Counter(job.status for job in jobs)
    log.info("POST /import: %s", dict(counts))
    return ImportResponse(jobs=jobs)


@router.get("/import/status", response_model=ImportStatusResponse, summary="Import progress")
def import_status(
    pmids: list[int] = Query(
        ..., description="PMIDs to report on. Repeatable.", min_length=1, max_length=MAX_BATCH
    ),
) -> ImportStatusResponse:
    """Report per-stage progress from `paper_stage_runs`.

    Read from the ledger rather than Celery's result backend: the ledger
    outlives `result_expires` and a worker restart, which is the point of
    having it. A PMID with no rows yet comes back with an empty `stages` map
    rather than being omitted, so the caller can tell "unknown" from "missing".
    """
    unique: list[int] = list(dict.fromkeys(pmids))
    with session_scope() as session:
        papers = persist.paper_progress(session, unique)
    return ImportStatusResponse(papers=papers)

