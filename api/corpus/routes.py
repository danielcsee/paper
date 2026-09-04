"""HTTP surface for the stored corpus, mounted at /corpus.

Sync (`def`, not `async def`) for the same reason as the ingestion routes: the
queries block on SQLAlchemy, so FastAPI runs them in its threadpool rather than
stalling the event loop.
"""

from __future__ import annotations

import logging
from math import ceil

from fastapi import APIRouter, HTTPException, Path, Query

from api.corpus import queries
from api.corpus.models import (
    DEFAULT_PAGE_SIZE,
    MAX_PAGE_SIZE,
    CorpusPage,
    CorpusPaperDetail,
)
from api.db import session_scope

log = logging.getLogger(__name__)
router = APIRouter(tags=["corpus"])


@router.get("/corpus", response_model=CorpusPage, summary="List imported papers")
def list_corpus(
    page: int = Query(1, ge=1, description="1-based page number."),
    page_size: int = Query(
        DEFAULT_PAGE_SIZE, ge=1, le=MAX_PAGE_SIZE, description="Papers per page."
    ),
) -> CorpusPage:
    """Papers that finished importing, most recent first.

    Only papers whose final stage is `done` appear. A paper that is queued,
    mid-import or failed has rows in `papers` already, but it is not in the
    corpus until it has content — so this joins the stage ledger rather than
    reading `papers` directly.

    A page beyond the end returns an empty list rather than a 404: the reader
    is scrolling, and running off the end is normal, not an error.
    """
    with session_scope() as session:
        total = queries.count_papers(session)
        papers = queries.list_papers(
            session, limit=page_size, offset=(page - 1) * page_size
        )

    return CorpusPage(
        page=page,
        page_size=page_size,
        total_papers=total,
        total_pages=ceil(total / page_size) if total else 0,
        papers=papers,
    )


@router.get(
    "/corpus/{paper_id}",
    response_model=CorpusPaperDetail,
    summary="Read one imported paper",
)
def read_paper(paper_id: int = Path(..., ge=1)) -> CorpusPaperDetail:
    """The whole paper: metadata, authors, paragraphs in order, references.

    404 for a paper that does not exist *or* has not finished importing — the
    reader must not be handed a half-ingested document.
    """
    with session_scope() as session:
        paper = queries.get_paper(session, paper_id)
    if paper is None:
        raise HTTPException(status_code=404, detail=f"paper {paper_id} is not in your corpus")
    return paper
