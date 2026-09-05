"""HTTP surface for the stored corpus, mounted at /corpus.

Sync (`def`, not `async def`) for the same reason as the ingestion routes: the
queries block on SQLAlchemy, so FastAPI runs them in its threadpool rather than
stalling the event loop.
"""

from __future__ import annotations

import logging
from math import ceil

from fastapi import APIRouter, HTTPException, Path, Query, Request

from api.corpus import queries, rag
from api.pb_client.errors import PbClientError
from api.corpus.models import (
    DEFAULT_PAGE_SIZE,
    MAX_PAGE_SIZE,
    CorpusPage,
    CorpusPaperDetail,
    RagSearchResponse,
    ReferenceList,
)
from api.app.config import get_settings
from api.ingestion.embedding import embed_query
from api.pb_client import PubTatorClient
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
    "/corpus/rag_search",
    response_model=RagSearchResponse,
    summary="Rank imported papers against a query",
)
def rag_search(
    query: str = Query(..., min_length=2, description="Free-text question."),
) -> RagSearchResponse:
    """Retrieval only: no LLM, no generated answer.

    Every stored chunk is scored against the query, anything below
    `rag_score_threshold` is discarded, the survivors are aggregated per paper,
    and the best few papers come back with their strongest excerpts.

    Registered before `/corpus/{paper_id}`: FastAPI matches routes in order,
    and "rag_search" against an int path parameter is a 422.
    """
    settings = get_settings()
    text = query.strip()
    if not text:
        raise HTTPException(status_code=400, detail="query must not be blank")

    vector = embed_query(text)
    with session_scope() as session:
        result = rag.search(
            session,
            text,
            vector,
            threshold=settings.rag_score_threshold,
            top_papers=settings.rag_top_papers,
            chunks_per_paper=settings.rag_chunks_per_paper,
        )
    log.info(
        "rag_search %r -> %d papers from %d chunks over %.2f",
        text,
        len(result.papers),
        result.chunks_considered,
        result.threshold,
    )
    return result


@router.get(
    "/corpus/{paper_id}/references",
    response_model=ReferenceList,
    summary="Importable references of a stored paper",
)
async def paper_references(
    request: Request, paper_id: int = Path(..., ge=1)
) -> ReferenceList:
    """The references of a stored paper that exist as full Papers in PubTator.

    Every entry returned is importable, so a count taken from the list is exact
    rather than an optimistic upper bound.

    This is the heavy call, and it has to be: the light export reports
    `pmcid: null` even for papers that *are* in PMC, so nothing cheaper can
    answer "does a full Paper exist". References without a PMID are skipped
    outright — the export is PMID-keyed and cannot be asked about them.

    Async, unlike its neighbours: the PubTator round-trip dominates, and
    holding a threadpool worker for several seconds of network wait would be
    the wrong resource to block.
    """
    with session_scope() as session:
        paper = queries.get_paper(session, paper_id)
        if paper is None:
            raise HTTPException(
                status_code=404, detail=f"paper {paper_id} is not in your corpus"
            )
        pmids, total, with_pmid = queries.reference_pmids(session, paper_id)

    truncated = len(pmids) > queries.MAX_REFERENCE_LOOKUP
    pmids = pmids[: queries.MAX_REFERENCE_LOOKUP]

    references: list = []
    if pmids:
        pubtator: PubTatorClient = request.app.state.pubtator
        try:
            fetched = await pubtator.fetch_papers(pmids, full=True)
        except PbClientError as exc:
            log.warning("references for paper %s failed: %s", paper_id, exc.message)
            raise HTTPException(status_code=exc.status, detail=exc.message) from exc
        references = [
            queries.to_search_result(item) for item in fetched if queries.is_importable(item)
        ]

    log.info(
        "paper %s references: %d total, %d with pmid, %d importable",
        paper_id,
        total,
        with_pmid,
        len(references),
    )
    return ReferenceList(
        paper_id=paper_id,
        pmid=paper.pmid,
        total_references=total,
        with_pmid=with_pmid,
        truncated=truncated,
        references=references,
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
