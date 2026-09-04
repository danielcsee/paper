"""Response models for the corpus routes."""

from __future__ import annotations

import datetime as dt
from typing import Optional

from pydantic import BaseModel, Field

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
