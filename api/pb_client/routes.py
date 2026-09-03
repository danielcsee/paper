"""HTTP surface for the NCBI clients, mounted at /pb."""

from __future__ import annotations

import logging
from typing import Optional

from fastapi import APIRouter, HTTPException, Query, Request

from api.pb_client.errors import PbClientError
from api.pb_client.models import DownloadResponse, FileKind, SearchResponse
from api.pb_client.pmc import ALL_KINDS, PmcClient
from api.pb_client.pubtator import PubTatorClient

log = logging.getLogger(__name__)
router = APIRouter(prefix="/pb", tags=["pb"])


def _fail(exc: PbClientError) -> HTTPException:
    return HTTPException(status_code=exc.status, detail=exc.message)


@router.get("/search", response_model=SearchResponse, summary="Search PubTator3 by text")
async def search(
    request: Request,
    text: str = Query(..., min_length=1, description="Free-text query."),
    page: int = Query(1, ge=1, description="1-based page. Upstream fixes page size at 10."),
) -> SearchResponse:
    client: PubTatorClient = request.app.state.pubtator
    try:
        return await client.search(text, page=page)
    except PbClientError as exc:
        log.warning("pb/search failed: %s", exc.message)
        raise _fail(exc) from exc


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
    except PbClientError as exc:
        log.warning("pb/download failed for %s: %s", pmcid, exc.message)
        raise _fail(exc) from exc
