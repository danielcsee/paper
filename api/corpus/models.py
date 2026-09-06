"""Response models for the corpus routes."""

from __future__ import annotations

import datetime as dt
from typing import Optional

from pydantic import BaseModel, Field

from api.pb_client.models import SearchResult

#: Matches the frontend's infinite-scroll page size. Capped so one request
#: cannot ask for the whole corpus.
DEFAULT_PAGE_SIZE = 20
MAX_PAGE_SIZE = 100


class CorpusPaper(BaseModel):
    """A stored paper, shaped for the same preview card as a search result."""

    paper_id: int
    pmid: int
    pmcid: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    pub_year: Optional[int] = None
    doi: Optional[str] = None
    authors: list[str] = Field(default_factory=list)
    #: First abstract chunk where there is one, else the first chunk. Gives the
    #: card the same shape as a search result's snippet.
    snippet: Optional[str] = None
    chunk_count: int = 0
    has_full_text: bool = False
    #: When the final stage completed — the ordering key.
    imported_at: Optional[dt.datetime] = None


class CorpusPage(BaseModel):
    page: int
    page_size: int
    total_papers: int
    total_pages: int
    papers: list[CorpusPaper]


class PaperParagraph(BaseModel):
    """One stored chunk, as a unit of the rendered document."""

    ordinal: int
    section_type: Optional[str] = None
    #: PubTator's passage kind. `is_heading` derives from it; it is kept so the
    #: reader can distinguish a figure caption or table caption from prose.
    chunk_type: Optional[str] = None
    text: str

    @property
    def is_heading(self) -> bool:
        return bool(self.chunk_type and "title" in self.chunk_type)


class PaperReferenceOut(BaseModel):
    ordinal: int
    title: Optional[str] = None
    pmid: Optional[str] = None
    doi: Optional[str] = None
    source: Optional[str] = None
    year: Optional[str] = None
    volume: Optional[str] = None
    fpage: Optional[str] = None
    lpage: Optional[str] = None


class CorpusPaperDetail(BaseModel):
    """A whole stored paper, enough to render it as a document."""

    paper_id: int
    pmid: int
    pmcid: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    journal_title: Optional[str] = None
    pub_year: Optional[int] = None
    volume: Optional[str] = None
    fpage: Optional[str] = None
    lpage: Optional[str] = None
    doi: Optional[str] = None
    has_full_text: bool = False
    imported_at: Optional[dt.datetime] = None
    authors: list[str] = Field(default_factory=list)
    #: In document order. The article title is excluded — it is `title`.
    paragraphs: list[PaperParagraph] = Field(default_factory=list)
    references: list[PaperReferenceOut] = Field(default_factory=list)


# --------------------------------------------------------------------------
# RAG search
# --------------------------------------------------------------------------


class RagChunk(BaseModel):
    """One matching excerpt, as evidence for why a paper ranked where it did."""

    chunk_id: int
    section_type: Optional[str] = None
    text: str
    score: float


class RagPaper(BaseModel):
    paper_id: int
    pmid: int
    pmcid: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    pub_year: Optional[int] = None
    #: The aggregated score the ranking used.
    score: float
    #: How many chunks cleared the threshold. With the `sum` aggregator this
    #: largely *is* the score, which is worth being able to see.
    matched_chunks: int
    best_score: float
    chunks: list[RagChunk] = Field(default_factory=list)


class RagSearchResponse(BaseModel):
    query: str
    #: Echoed so a caller can tell "nothing matched" from "the bar was high".
    threshold: float
    aggregator: str
    chunks_considered: int
    papers: list[RagPaper] = Field(default_factory=list)


class ReferenceList(BaseModel):
    """The importable references of a stored paper.

    Every entry is guaranteed to exist as a full Paper in PubTator, so a count
    taken from `references` is exact rather than optimistic.
    """

    paper_id: int
    pmid: int
    #: References recorded for the paper, importable or not.
    total_references: int
    #: Those carrying a PMID, i.e. the ones we could even ask PubTator about.
    with_pmid: int
    #: True when the reference list was longer than we were willing to query.
    truncated: bool = False
    #: Importable references, shaped as search results so the UI renders them
    #: with the same card. `score` and `text_hl` are search-only and stay null.
    references: list[SearchResult] = Field(default_factory=list)


class EntitySpan(BaseModel):
    """Where one mention sits in the rendered document.

    `start` is relative to the paragraph, not the document: the client slices
    `paragraph.text` directly and never learns the document coordinate space.

    `text` is what should be at that slice. Carrying it lets the client verify
    before highlighting — Postgres counts characters where JavaScript counts
    UTF-16 units, so a non-BMP character anywhere earlier in the paragraph
    would shift every later span. This corpus is pure ASCII today, which is a
    property of the data rather than a guarantee.
    """

    #: `PaperParagraph.ordinal`, so the client needs no chunk ids.
    ordinal: int
    start: int
    length: int
    text: str


class PaperEntityItem(BaseModel):
    """One grounded concept found in a paper, with how it was written there."""

    entity_id: int
    identifier: str
    entity_type: str
    database: str
    #: PubTator's canonical name. For Species it is the taxon number, which is
    #: why `names` matters: "9685" is not what anyone calls a cat.
    name: Optional[str] = None
    #: Distinct surface forms this paper used, most frequent first.
    names: list[str] = Field(default_factory=list)
    mention_count: int
    #: Every occurrence, in reading order.
    spans: list[EntitySpan] = Field(default_factory=list)


class PaperEntityList(BaseModel):
    paper_id: int
    total: int
    entities: list[PaperEntityItem] = Field(default_factory=list)
