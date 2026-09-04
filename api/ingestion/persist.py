"""Write a fetched paper into Postgres.

Split from `tasks.py` so the mapping logic is testable without a broker. Every
function is idempotent: re-importing a paper updates rather than duplicates,
which is what the unique constraints in `api.db.models` are there to enforce.
"""

from __future__ import annotations

from typing import Literal, Optional, Sequence

from sqlalchemy.orm import Session

from api.ingestion.models import PaperProgress
from api.pb_client.models import PaperResponse

#: The stage whose completion means a paper is fully imported. Kept as a named
#: constant because /import's "already_imported" check depends on it, and the
#: chain's last stage is the only honest answer to "is this done?".
FINAL_STAGE = "embed"

#: Stage statuses that mean a chain is still working on a paper.
ACTIVE_STATUSES = ("pending", "running")

#: What the ledger says about a paper that already has rows. Deliberately not
#: the API's vocabulary — routes.py maps these onto ImportStatus.
LedgerState = Literal["complete", "in_progress"]


def import_states(session: Session, pmids: Sequence[int]) -> dict[int, LedgerState]:
    """What the ledger already knows about these PMIDs.

    Only papers in one of two states appear; anything absent should be queued.

    * ``complete``    — the `FINAL_STAGE` row is 'done'.
    * ``in_progress`` — not complete, but some stage is 'pending' or 'running',
      so a chain is still working on it. Queueing another would put two chains
      on the same rows concurrently.

    A paper whose stages all *failed* is in neither state: re-queueing it is
    the correct response, and each task self-skips the stages already current.
    """
    raise NotImplementedError


def paper_progress(session: Session, pmids: Sequence[int]) -> list[PaperProgress]:
    """Per-stage state for each PMID, in the order given.

    A PMID with no `papers` row yet still yields an entry, with `paper_id` None
    and an empty `stages` map, so callers can distinguish "not started" from
    "not asked about". `error` carries the message from the first failed stage.
    """
    raise NotImplementedError


def upsert_paper(session: Session, paper: PaperResponse) -> int:
    """Insert or update the `papers` row, returning its id.

    `INSERT ... ON CONFLICT (pmid) DO UPDATE ... RETURNING id`, so two workers
    importing the same PMID cannot race into a constraint violation.
    """
    raise NotImplementedError


def store_raw_document(session: Session, paper_id: int, raw: dict) -> None:
    """Persist the verbatim PubTator response.

    This is what lets the later stages re-run without touching the network, and
    what makes re-chunking a local operation.
    """
    raise NotImplementedError


def load_raw_document(session: Session, paper_id: int) -> dict:
    """Read back what `store_raw_document` wrote."""
    raise NotImplementedError


def replace_authors(session: Session, paper_id: int, paper: PaperResponse) -> int:
    raise NotImplementedError


def replace_references(session: Session, paper_id: int, paper: PaperResponse) -> int:
    raise NotImplementedError


def upsert_entities(session: Session, paper: PaperResponse) -> dict[str, int]:
    """Ensure an `entities` row per grounded concept; return identifier -> id.

    Ungrounded annotations (upstream identifier "-") are dropped here — they
    fragment the graph, which is the whole reason for grounding entities.
    """
    raise NotImplementedError


def replace_chunks(session: Session, paper_id: int, paper: PaperResponse) -> list[int]:
    """Write `paper_chunks` (text and offsets, no vectors) and return their ids.

    Mentions are re-pointed at the new chunks afterwards rather than deleted:
    `chunk_id` is ON DELETE SET NULL precisely so re-chunking cannot destroy
    mention data, which is expensive to re-acquire.
    """
    raise NotImplementedError


def replace_mentions(
    session: Session, paper_id: int, paper: PaperResponse, entity_ids: dict[str, int]
) -> int:
    raise NotImplementedError


def replace_relations(
    session: Session, paper_id: int, paper: PaperResponse, entity_ids: dict[str, int]
) -> int:
    raise NotImplementedError


def mark_stage(
    session: Session,
    paper_id: int,
    stage: str,
    status: str,
    *,
    error: Optional[str] = None,
    fingerprint: Optional[str] = None,
) -> None:
    """Upsert the `paper_stage_runs` row for one stage.

    Called with 'pending' before any external work, so an outage costs delay
    rather than data.
    """
    raise NotImplementedError


def stage_is_current(
    session: Session, paper_id: int, stage: str, fingerprint: Optional[str]
) -> bool:
    """True when the stage is 'done' for this fingerprint, so it can be skipped."""
    raise NotImplementedError
