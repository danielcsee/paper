"""Request and response models for the /import routes."""

from __future__ import annotations

from typing import Literal, Optional

from pydantic import BaseModel, Field

from api.pb_client.models import SearchResult

ImportStatus = Literal["queued", "already_imported", "rejected"]


class ImportRequest(BaseModel):
    """The selected search results, exactly as the sidebar holds them.

    Only `pmid` is actually needed, but accepting whole results keeps the
    frontend from having to reshape its own state, and leaves room to record
    the query a paper was imported from later.
    """

    papers: list[SearchResult] = Field(..., min_length=1)


class ImportJob(BaseModel):
    pmid: Optional[int] = None
    status: ImportStatus
    task_id: Optional[str] = None
    #: Why a paper was rejected — no PMID, for instance.
    reason: Optional[str] = None


class ImportResponse(BaseModel):
    jobs: list[ImportJob]


class PaperProgress(BaseModel):
    """Per-stage state, read straight from `paper_stage_runs`."""

    pmid: int
    paper_id: Optional[int] = None
    stages: dict[str, str] = Field(default_factory=dict)
    error: Optional[str] = None


class ImportStatusResponse(BaseModel):
    papers: list[PaperProgress]
