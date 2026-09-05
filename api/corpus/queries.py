"""Reads over the stored corpus.

Separate from `routes.py` so the query logic is testable without HTTP, the same
split `api.ingestion.persist` uses.
"""

from __future__ import annotations

from typing import Optional, Sequence

from sqlalchemy import Select, func, select
from sqlalchemy.orm import Session

from api.corpus.models import (
    CorpusPaper,
    CorpusPaperDetail,
    PaperParagraph,
    PaperReferenceOut,
)
from api.pb_client.models import PaperResponse, SearchResult
from api.db.models import (
    Paper,
    PaperAuthor,
    PaperChunk,
    PaperReference,
    PaperStageRun,
)

#: How much of a chunk to keep for the preview card.
SNIPPET_CHARS = 240


def _imported_papers() -> Select:
    """Papers whose final stage completed — the definition of 'in my corpus'.

    Joining the ledger rather than reading `papers` directly matters: a row
    exists there from the moment `/import` reserves it, long before the paper
    has any content.
    """
    return select(Paper, PaperStageRun.finished_at).join(
        PaperStageRun,
        (PaperStageRun.paper_id == Paper.id)
        & (PaperStageRun.stage == PaperStageRun.FINAL_STAGE)
        & (PaperStageRun.status == "done"),
    )


def count_papers(session: Session) -> int:
    return session.scalar(
        select(func.count()).select_from(_imported_papers().subquery())
    ) or 0


def list_papers(session: Session, *, limit: int, offset: int) -> list[CorpusPaper]:
    """One page, most recently imported first.

    `Paper.id` breaks ties. Without a total order two papers finishing in the
    same instant could appear on two pages or neither as the reader scrolls.
    """
    rows = session.execute(
        _imported_papers()
        .order_by(PaperStageRun.finished_at.desc().nullslast(), Paper.id.desc())
        .limit(limit)
        .offset(offset)
    ).all()
    if not rows:
        return []

    papers = [row[0] for row in rows]
    ids = [paper.id for paper in papers]
    authors = _authors_by_paper(session, ids)
    snippets = _snippets_by_paper(session, ids)
    counts = _chunk_counts_by_paper(session, ids)

    return [
        CorpusPaper(
            paper_id=paper.id,
            pmid=paper.pmid,
            pmcid=paper.pmcid,
            title=paper.title,
            journal=paper.journal,
            pub_year=paper.pub_year,
            doi=paper.doi,
            authors=authors.get(paper.id, []),
            snippet=snippets.get(paper.id),
            chunk_count=counts.get(paper.id, 0),
            has_full_text=paper.has_full_text,
            imported_at=finished_at,
        )
        for paper, finished_at in rows
    ]


def _authors_by_paper(session: Session, ids: Sequence[int]) -> dict[int, list[str]]:
    """One query for the page, not one per paper."""
    rows = session.execute(
        select(PaperAuthor.paper_id, PaperAuthor.surname, PaperAuthor.given_names)
        .where(PaperAuthor.paper_id.in_(list(ids)))
        .order_by(PaperAuthor.paper_id, PaperAuthor.ordinal)
    ).all()
    out: dict[int, list[str]] = {}
    for paper_id, surname, given_names in rows:
        name = " ".join(part for part in (surname, given_names) if part)
        if name:
            out.setdefault(paper_id, []).append(name)
    return out


def _snippets_by_paper(session: Session, ids: Sequence[int]) -> dict[int, str]:
    """Prefer an abstract chunk; fall back to the first chunk of anything."""
    rows = session.execute(
        select(PaperChunk.paper_id, PaperChunk.section_type, PaperChunk.text)
        .where(PaperChunk.paper_id.in_(list(ids)))
        .order_by(PaperChunk.paper_id, PaperChunk.ordinal)
    ).all()
    first: dict[int, str] = {}
    abstract: dict[int, str] = {}
    for paper_id, section_type, text in rows:
        if paper_id not in first:
            first[paper_id] = text
        if section_type == "ABSTRACT" and paper_id not in abstract:
            abstract[paper_id] = text
    return {
        paper_id: _truncate(abstract.get(paper_id) or first[paper_id])
        for paper_id in first
    }


def _chunk_counts_by_paper(session: Session, ids: Sequence[int]) -> dict[int, int]:
    rows = session.execute(
        select(PaperChunk.paper_id, func.count())
        .where(PaperChunk.paper_id.in_(list(ids)))
        .group_by(PaperChunk.paper_id)
    ).all()
    return {paper_id: count for paper_id, count in rows}


def _truncate(text: Optional[str]) -> Optional[str]:
    if not text:
        return None
    collapsed = " ".join(text.split())
    if len(collapsed) <= SNIPPET_CHARS:
        return collapsed
    return collapsed[:SNIPPET_CHARS].rstrip() + "…"


def get_paper(session: Session, paper_id: int) -> Optional[CorpusPaperDetail]:
    """One whole paper, or None when it is absent or not finished importing.

    Same membership rule as the listing: a paper reserved by `/import` but not
    yet ingested exists in `papers` and must not be readable.
    """
    row = session.execute(
        _imported_papers().where(Paper.id == paper_id)
    ).first()
    if row is None:
        return None
    paper, finished_at = row

    chunks = session.execute(
        select(PaperChunk.ordinal, PaperChunk.section_type, PaperChunk.chunk_type, PaperChunk.text)
        .where(PaperChunk.paper_id == paper_id)
        .order_by(PaperChunk.ordinal)
    ).all()

    references = session.execute(
        select(PaperReference)
        .where(PaperReference.paper_id == paper_id)
        .order_by(PaperReference.ordinal)
    ).scalars().all()

    return CorpusPaperDetail(
        paper_id=paper.id,
        pmid=paper.pmid,
        pmcid=paper.pmcid,
        title=paper.title,
        journal=paper.journal,
        journal_title=paper.journal_title,
        pub_year=paper.pub_year,
        volume=paper.volume,
        fpage=paper.fpage,
        lpage=paper.lpage,
        doi=paper.doi,
        has_full_text=paper.has_full_text,
        imported_at=finished_at,
        authors=_authors_by_paper(session, [paper_id]).get(paper_id, []),
        paragraphs=[
            PaperParagraph(
                ordinal=ordinal, section_type=section, chunk_type=kind, text=text
            )
            # The article title is rendered from `title`; repeating it as the
            # first paragraph would print it twice.
            for ordinal, section, kind, text in chunks
            if kind != "front"
        ],
        references=[
            PaperReferenceOut(
                ordinal=r.ordinal,
                title=r.title,
                pmid=r.ref_pmid,
                doi=r.ref_doi,
                source=r.source,
                year=r.year,
                volume=r.volume,
                fpage=r.fpage,
                lpage=r.lpage,
            )
            for r in references
        ],
    )


#: Refuse to query more than this many references for one paper. The export
#: endpoint caps a single request at 100, and chunking beyond a few calls turns
#: one click into a long rate-limited stall.
MAX_REFERENCE_LOOKUP = 300

#: Characters of abstract kept as the card's snippet.
REFERENCE_SNIPPET_CHARS = 260


def reference_pmids(session: Session, paper_id: int) -> tuple[list[int], int, int]:
    """(pmids to look up, total references, references carrying a PMID).

    Only references with a PMID can be asked about at all — PubTator's export
    is PMID-keyed. Measured on this corpus the share ranges from 0% to 93%.
    """
    rows = session.execute(
        select(PaperReference.ref_pmid)
        .where(PaperReference.paper_id == paper_id)
        .order_by(PaperReference.ordinal)
    ).scalars().all()

    total = len(rows)
    pmids: list[int] = []
    seen: set[int] = set()
    for value in rows:
        if not value or not value.isdigit():
            continue
        pmid = int(value)
        if pmid in seen:
            continue
        seen.add(pmid)
        pmids.append(pmid)
    return pmids, total, len(pmids)


def is_importable(paper: PaperResponse) -> bool:
    """True when PubTator holds a full Paper for this reference.

    Both halves are checked deliberately. `pmcid` alone is not enough: it is
    only ever populated when full text is actually returned, so it says nothing
    on its own about a light response. `has_full_text` alone would admit a
    document with body text but no PMC identity to import against.
    """
    return bool(paper.pmcid) and paper.has_full_text


def to_search_result(paper: PaperResponse) -> SearchResult:
    """Shape a fetched paper like a search hit, so the UI reuses one card.

    `score` and `text_hl` stay null: they are relevance artefacts of a search,
    and this is a reference list. The abstract stands in for the snippet.
    """
    # `Passage.type` is PubTator's passage kind; `chunk_type` is the database
    # column that stores it. Abstract *headings* ("Background") are
    # abstract_title_1 and make a poor snippet, so prefer the prose.
    abstract = next((p.text for p in paper.passages if p.type == "abstract"), None)
    if abstract is None:
        abstract = next(
            (
                p.text
                for p in paper.passages
                if (p.section_type or "").upper() == "ABSTRACT"
                and (p.type or "") != "abstract_title_1"
            ),
            None,
        )
    snippet = None
    if abstract:
        collapsed = " ".join(abstract.split())
        snippet = (
            collapsed
            if len(collapsed) <= REFERENCE_SNIPPET_CHARS
            else collapsed[:REFERENCE_SNIPPET_CHARS].rstrip() + "\u2026"
        )

    return SearchResult(
        pmid=paper.pmid,
        pmcid=paper.pmcid,
        title=paper.title,
        journal=paper.journal,
        authors=[author.display for author in paper.authors],
        date=paper.date,
        doi=paper.doi,
        score=None,
        text_hl=None,
        snippet=snippet,
    )
