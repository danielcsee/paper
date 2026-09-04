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
