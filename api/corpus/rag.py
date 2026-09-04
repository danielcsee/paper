"""Retrieval over stored chunk embeddings.

Retrieval only — nothing here calls an LLM. A search returns the papers whose
chunks best match the query, with the matching excerpts as evidence.

The shape is deliberately simple:

    embed query -> score every chunk -> drop the weak ones
                -> aggregate per paper -> take the top few

Aggregation is a named function rather than an inline `sum()` so the choice can
be compared against alternatives without touching the query path. See
`AGGREGATORS`.
"""

from __future__ import annotations

from typing import Callable, Optional, Sequence

from sqlalchemy import Float, bindparam, func, select
from sqlalchemy.orm import Session

from api.corpus.models import RagChunk, RagPaper, RagSearchResponse
from api.db.models import Paper, PaperChunk, PaperStageRun

#: How a paper's overall score is built from its surviving chunk scores.
Aggregator = Callable[[Sequence[float]], float]


def aggregate_sum(scores: Sequence[float]) -> float:
    """Total of the surviving chunk scores.

    Note this rewards length: a paper with twice the chunks has roughly twice
    the ceiling at equal relevance, so a long tangential paper can outrank a
    short exact one. Measured on this corpus, the mean surviving score barely
    differs between a matching and a non-matching paper (0.68 vs 0.66) — nearly
    all the separation comes from how *many* chunks survive. Kept as the
    default by choice; swap it here when there is a corpus big enough to judge.
    """
    return float(sum(scores))


def aggregate_max(scores: Sequence[float]) -> float:
    """Best single chunk. Length-neutral, but ignores corroboration."""
    return float(max(scores)) if scores else 0.0


def aggregate_mean(scores: Sequence[float]) -> float:
    """Average of the survivors. Length-neutral; a single strong chunk can win."""
    return float(sum(scores) / len(scores)) if scores else 0.0


AGGREGATORS: dict[str, Aggregator] = {
    "sum": aggregate_sum,
    "max": aggregate_max,
    "mean": aggregate_mean,
}

DEFAULT_AGGREGATOR = "sum"


def score_chunks(
    session: Session, query_vector: Sequence[float], threshold: float
) -> list[tuple[int, int, Optional[str], str, float]]:
    """Every chunk at or above `threshold`, best first.

    `1 - cosine_distance` because the caller wants higher-is-better, and the
    embeddings are unit-normalised so this is an exact cosine similarity.

    An exhaustive scan, not an index lookup. The HNSW index answers "the
    nearest k", which cannot express "everything above a cutoff"; at this
    corpus size the scan is trivial, and switching to over-fetching k is the
    change to make when it stops being.
    """
    vector = bindparam("q", value=list(query_vector), type_=PaperChunk.embedding.type)
    similarity = (1 - PaperChunk.embedding.cosine_distance(vector)).cast(Float)

    rows = session.execute(
        select(
            PaperChunk.paper_id,
            PaperChunk.id,
            PaperChunk.section_type,
            PaperChunk.text,
            similarity.label("score"),
        )
        .join(Paper, Paper.id == PaperChunk.paper_id)
        # Same membership rule as the rest of the corpus: a paper only counts
        # once it has finished importing.
        .join(
            PaperStageRun,
            (PaperStageRun.paper_id == Paper.id)
            & (PaperStageRun.stage == PaperStageRun.FINAL_STAGE)
            & (PaperStageRun.status == "done"),
        )
        .where(PaperChunk.embedding.isnot(None))
        .where(similarity >= threshold)
        .order_by(similarity.desc())
    ).all()
    return [(r.paper_id, r.id, r.section_type, r.text, float(r.score)) for r in rows]


def search(
    session: Session,
    query: str,
    query_vector: Sequence[float],
    *,
    threshold: float,
    top_papers: int,
    chunks_per_paper: int,
    aggregator: str = DEFAULT_AGGREGATOR,
) -> RagSearchResponse:
    """Rank papers for one query."""
    aggregate = AGGREGATORS[aggregator]
    scored = score_chunks(session, query_vector, threshold)

    by_paper: dict[int, list[tuple[int, Optional[str], str, float]]] = {}
    for paper_id, chunk_id, section_type, text, score in scored:
        by_paper.setdefault(paper_id, []).append((chunk_id, section_type, text, score))

    if not by_paper:
        return RagSearchResponse(
            query=query,
            threshold=threshold,
            aggregator=aggregator,
            chunks_considered=0,
            papers=[],
        )

    ranked = sorted(
        by_paper.items(),
        key=lambda item: aggregate([score for *_, score in item[1]]),
        reverse=True,
    )[:top_papers]

    metadata = {
        paper.id: paper
        for paper in session.scalars(
            select(Paper).where(Paper.id.in_([paper_id for paper_id, _ in ranked]))
        )
    }

    papers: list[RagPaper] = []
    for paper_id, matches in ranked:
        paper = metadata[paper_id]
        scores = [score for *_, score in matches]
        papers.append(
            RagPaper(
                paper_id=paper_id,
                pmid=paper.pmid,
                pmcid=paper.pmcid,
                title=paper.title,
                journal=paper.journal,
                pub_year=paper.pub_year,
                score=round(aggregate(scores), 6),
                matched_chunks=len(matches),
                best_score=round(max(scores), 6),
                chunks=[
                    RagChunk(
                        chunk_id=chunk_id,
                        section_type=section_type,
                        text=text,
                        score=round(score, 6),
                    )
                    # `matches` is already best-first: score_chunks ordered it.
                    for chunk_id, section_type, text, score in matches[:chunks_per_paper]
                ],
            )
        )

    return RagSearchResponse(
        query=query,
        threshold=threshold,
        aggregator=aggregator,
        chunks_considered=len(scored),
        papers=papers,
    )


def paper_count(session: Session) -> int:
    """Papers eligible to be searched, for an honest empty-result message."""
    return session.scalar(
        select(func.count())
        .select_from(Paper)
        .join(
            PaperStageRun,
            (PaperStageRun.paper_id == Paper.id)
            & (PaperStageRun.stage == PaperStageRun.FINAL_STAGE)
            & (PaperStageRun.status == "done"),
        )
    ) or 0
