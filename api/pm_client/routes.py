"""HTTP surface for the PMC Open Access client, mounted at /pm."""

from __future__ import annotations

import logging
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Query, Request

from api.auth import require_user

from api.ncbi.errors import NcbiError
from api.pm_client.models import DownloadResponse, FileKind
from api.pm_client.pmc import ALL_KINDS, PmcClient

log = logging.getLogger(__name__)
# Downloads pull article files from PMC under the shared NCBI budget, so the
# whole router is gated.
router = APIRouter(prefix="/pm", tags=["pm"], dependencies=[Depends(require_user)])


@router.get("/download", response_model=DownloadResponse, summary="Download a paper from PMC")
async def download(
    request: Request,
    pmcid: str = Query(..., description="PMCID, e.g. PMC107028 (a bare number is accepted)."),
    kinds: Optional[list[FileKind]] = Query(
        None, description="Which files to fetch. Repeatable. Defaults to all."
    ),
    dry_run: bool = Query(False, description="Report what would be fetched, write nothing."),
    overwrite: bool = Query(False, description="Re-download files already on disk."),
) -> DownloadResponse:
    client: PmcClient = request.app.state.pmc
    try:
        return await client.download(
            pmcid, kinds=kinds or ALL_KINDS, dry_run=dry_run, overwrite=overwrite
        )
    except NcbiError as exc:
        log.warning("pm/download failed for %s: %s", pmcid, exc.message)
        raise HTTPException(status_code=exc.status, detail=exc.message) from exc
