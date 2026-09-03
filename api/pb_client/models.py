"""Response models. These are ours, not NCBI's — upstream fields are normalised
here so callers don't depend on the shape of either service's JSON."""

from typing import Literal, Optional

from pydantic import BaseModel, Field

FileKind = Literal["xml", "text", "pdf", "media"]


class SearchResult(BaseModel):
    pmid: Optional[int] = None
    pmcid: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    authors: list[str] = Field(default_factory=list)
    date: Optional[str] = None
    doi: Optional[str] = None
    score: Optional[float] = None
    #: Upstream highlight string, with PubTator's inline entity markup intact
    #: (e.g. "@GENE_BRCA1 @GENE_672 @@@<m>BRCA1</m>@@@").
    text_hl: Optional[str] = None
    #: `text_hl` with that markup stripped — safe to render directly.
    snippet: Optional[str] = None


class SearchResponse(BaseModel):
    query: str
    page: int
    page_size: int
    total_results: int
    total_pages: int
    results: list[SearchResult]


class DownloadedFile(BaseModel):
    kind: FileKind
    filename: str
    url: str
    bytes: int
    #: True when the file was already on disk and was left alone.
    skipped: bool = False


class DownloadResponse(BaseModel):
    pmcid: str
    version: int
    pmid: Optional[int] = None
    doi: Optional[str] = None
    title: Optional[str] = None
    citation: Optional[str] = None
    license_code: Optional[str] = None
    is_open_access: bool = False
    is_retracted: bool = False
    #: Absolute directory the files were written to. None for a dry run.
    directory: Optional[str] = None
    files: list[DownloadedFile] = Field(default_factory=list)
