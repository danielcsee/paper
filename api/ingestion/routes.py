"""HTTP surface for the ingestion pipeline, mounted at /import."""

from __future__ import annotations

import logging

from fastapi import APIRouter, Query

from api.ingestion.models import ImportRequest, ImportResponse, ImportStatusResponse

log = logging.getLogger(__name__)
router = APIRouter(tags=["import"])


@router.post("/import", response_model=ImportResponse, status_code=202, summary="Queue papers")
async def import_papers(request: ImportRequest) -> ImportResponse:
    """Kick off one chain per selected paper.

    Returns immediately with a job per paper. A result with no PMID is rejected
    individually rather than failing the whole batch, and one already imported
    is reported as such instead of being re-queued.
    """
    raise NotImplementedError


@router.get("/import/status", response_model=ImportStatusResponse, summary="Import progress")
async def import_status(
    pmids: list[int] = Query(..., description="PMIDs to report on. Repeatable."),
) -> ImportStatusResponse:
    """Report per-stage progress from `paper_stage_runs`.

    Read from the ledger rather than Celery's result backend: the ledger
    survives result expiry and a worker restart, which is the point of having it.
    """
    raise NotImplementedError
