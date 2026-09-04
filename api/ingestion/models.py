"""Request and response models for the /import routes."""

from __future__ import annotations

from typing import Literal, Optional

from pydantic import BaseModel, Field

from api.pb_client.models import SearchResult

ImportStatus = Literal["queued", "in_progress", "already_imported", "rejected"]

#: One paper's overall progress, collapsed from its per-stage ledger rows.
#: Derived here rather than in the UI: "which stage is last" is a backend fact,
#: and adding the Neo4j `graph` stage must not silently change what a client
#: considers finished.
PaperState = Literal["queued", "started", "success", "error"]

#: Cap on one request. Every queued paper becomes a PubTator fetch, and the
#: worker is rate-limited to ~3/s, so an unbounded batch is a long queue rather
#: than a fast import.
MAX_BATCH = 100


class ImportRequest(BaseModel):
    """The selected search results, exactly as the sidebar holds them.

    Only `pmid` is actually needed, but accepting whole results keeps the
    frontend from having to reshape its own state, and leaves room to record
    the query a paper was imported from later.
    """

    papers: list[SearchResult] = Field(..., min_length=1, max_length=MAX_BATCH)
    #: Re-import regardless of ledger state, including papers still in flight.
    #: The escape hatch for a chain that died without marking itself failed.
    force: bool = False


class ImportJob(BaseModel):
    """One paper's outcome.

    `in_progress` means an earlier chain is still working on it, so nothing was
    queued — re-queueing would put two chains on the same rows at once.
    """

    pmid: Optional[int] = None
    status: ImportStatus
    #: Set only for `queued`; we do not persist the task id of earlier runs.
    task_id: Optional[str] = None
    #: Why a paper was rejected — no PMID, for instance.
    reason: Optional[str] = None


class ImportResponse(BaseModel):
    jobs: list[ImportJob]


class PaperProgress(BaseModel):
    """Per-stage state, read straight from `paper_stage_runs`."""

    pmid: int
    paper_id: Optional[int] = None
    #: Raw ledger rows, kept for debugging and for anything that wants detail.
    stages: dict[str, str] = Field(default_factory=dict)
    #: The collapsed view a client should render.
    state: PaperState = "queued"
    error: Optional[str] = None


class ImportStatusResponse(BaseModel):
    papers: list[PaperProgress]
