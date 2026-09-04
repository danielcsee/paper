"""HTTP surface for the ingestion pipeline, mounted at /import.

Both routes are sync (`def`, not `async def`) on purpose. They talk to Postgres
through SQLAlchemy's blocking API, so FastAPI runs them in its threadpool; an
`async def` would block the event loop for the whole query.
"""

from __future__ import annotations

import logging
from typing import Optional

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

    * **rejected** — no PMID. PubTator's export endpoint is keyed on PMID, so
      there is nothing to fetch. Rejected per item rather than failing the whole
      batch, since a mixed selection is normal.
    * **already_imported** — the paper's final stage is `done`. Pass
      `force: true` to re-run anyway.
    * **queued** — a chain was submitted; `task_id` is the chain's id.

    A PMID repeated inside one request is queued once.
    """
    pmids: list[int] = [p.pmid for p in request.papers if p.pmid is not None]

    completed: set[int] = set()
    if pmids and not request.force:
        with session_scope() as session:
            completed = persist.completed_pmids(session, pmids)

    jobs: list[ImportJob] = []
    queued: dict[int, str] = {}

    for paper in request.papers:
        if paper.pmid is None:
            jobs.append(
                ImportJob(
                    pmid=None,
                    status="rejected",
                    reason="no PMID; PubTator cannot be queried without one",
                )
            )
            continue

        if paper.pmid in completed:
            jobs.append(ImportJob(pmid=paper.pmid, status="already_imported"))
            continue

        # Same PMID twice in one selection: report both, queue one.
        if paper.pmid in queued:
            jobs.append(ImportJob(pmid=paper.pmid, status="queued", task_id=queued[paper.pmid]))
            continue

        try:
            result = import_paper(paper.pmid, force=request.force)
        except NotImplementedError:
            raise
        except Exception as exc:  # broker unreachable, mainly
            log.exception("could not queue PMID %s", paper.pmid)
            raise HTTPException(
                status_code=503, detail=f"could not queue import: {exc}"
            ) from exc

        task_id = getattr(result, "id", None)
        if task_id is not None:
            queued[paper.pmid] = task_id
        jobs.append(ImportJob(pmid=paper.pmid, status="queued", task_id=task_id))

    log.info(
        "POST /import: %d queued, %d already imported, %d rejected",
        sum(1 for j in jobs if j.status == "queued"),
        sum(1 for j in jobs if j.status == "already_imported"),
        sum(1 for j in jobs if j.status == "rejected"),
    )
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

