"""HTTP surface for the PubTator3 client, mounted at /pb."""

from __future__ import annotations

import logging
from fastapi import APIRouter, Depends, HTTPException, Query, Request

from api.auth import require_user

from api.ncbi.errors import NcbiError
from api.pb_client.models import PaperResponse, SearchResponse
from api.pb_client.pubtator import PubTatorClient

log = logging.getLogger(__name__)
# Every route here calls PubTator, which is rate-limited for the whole
# organisation. Gated at the router so a new /pb route cannot be added
# unprotected by accident.
router = APIRouter(prefix="/pb", tags=["pb"], dependencies=[Depends(require_user)])


def _fail(exc: NcbiError) -> HTTPException:
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
    except NcbiError as exc:
        log.warning("pb/search failed: %s", exc.message)
        raise _fail(exc) from exc


@router.get(
    "/paper",
    response_model=PaperResponse,
    summary="Fetch a full annotated paper from PubTator by PMID",
)
async def paper(
    request: Request,
    pmid: int = Query(..., ge=1, description="PMID. PubTator is keyed on it."),
    full: bool = Query(True, description="Full text. False returns title + abstract only."),
    include_ref_passages: bool = Query(
        False,
        description="Keep bibliography entries in `passages` too. They are always "
        "returned, structured, under `references`.",
    ),
) -> PaperResponse:
    """PMID only.

    A paper has full text in PubTator exactly when it is also in PMC, and every
    such paper carries both ids in its search result — so callers always have a
    PMID and there is nothing to resolve.
    """
    pubtator: PubTatorClient = request.app.state.pubtator
    try:
        return await pubtator.fetch_paper(
            pmid, full=full, include_ref_passages=include_ref_passages
        )
    except NcbiError as exc:
        log.warning("pb/paper failed for pmid=%s: %s", pmid, exc.message)
        raise _fail(exc) from exc
