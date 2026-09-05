"""Response models for the PMC download. Ours, not NCBI's — the bucket's
metadata JSON is normalised here so callers don't depend on its shape."""

from typing import Literal, Optional

from pydantic import BaseModel, Field

FileKind = Literal["xml", "text", "pdf", "media"]


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
