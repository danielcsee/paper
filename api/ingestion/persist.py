"""Write a fetched paper into Postgres.

Split from `tasks.py` so the mapping logic is testable without a broker. Every
function is idempotent: re-importing a paper updates rather than duplicates,
which is what the unique constraints in `api.db.models` enforce.

Child rows use replace-semantics — delete this paper's rows, insert the new
ones — because upstream can revise a paper and a diff would be more code for no
benefit. The exception is `paper_entity_mentions`, which is re-pointed rather
than rebuilt, and `entities`, which is shared across papers.
"""

from __future__ import annotations

import datetime as dt
import logging
from typing import Literal, Optional, Sequence

from sqlalchemy import delete, func, select
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.orm import Session

from api.db.models import (
    Entity,
    Paper,
    PaperAuthor,
    PaperChunk,
    PaperEntityMention,
    PaperPubtatorDoc,
    PaperReference,
    PaperRelation,
    PaperStageRun,
)
from api.ingestion.chunking import Chunk, build_body_text, chunks_from_passages, find_chunk_ordinal
from api.ingestion.models import PaperProgress, PaperState
from api.pb_client.models import PaperResponse, normalise_identifier

log = logging.getLogger(__name__)

#: Re-exported for readability at the call sites in this module. The single
#: definition lives on the model, because `api.corpus` reads it too.
FINAL_STAGE = PaperStageRun.FINAL_STAGE

#: Stage statuses that mean a chain is still working on a paper.
ACTIVE_STATUSES = ("pending", "running")

#: What the ledger says about a paper that already has rows. Deliberately not
#: the API's vocabulary — routes.py maps these onto ImportStatus.
LedgerState = Literal["complete", "in_progress"]


# ---------------------------------------------------------------- ledger reads


def import_states(session: Session, pmids: Sequence[int]) -> dict[int, LedgerState]:
    """What the ledger already knows about these PMIDs.

    Only papers in one of two states appear; anything absent should be queued.

    * ``complete``    — the `FINAL_STAGE` row is 'done'.
    * ``in_progress`` — not complete, but some stage is 'pending' or 'running'.

    A paper whose stages all *failed* is in neither state: re-queueing it is the
    correct response, and each task self-skips the stages already current.
    """
    if not pmids:
        return {}

    rows = session.execute(
        select(Paper.pmid, PaperStageRun.stage, PaperStageRun.status)
        .join(PaperStageRun, PaperStageRun.paper_id == Paper.id)
        .where(Paper.pmid.in_(list(pmids)))
    ).all()

    states: dict[int, LedgerState] = {}
    for pmid, stage, status in rows:
        if stage == FINAL_STAGE and status == "done":
            states[pmid] = "complete"
        elif status in ACTIVE_STATUSES and states.get(pmid) != "complete":
            states[pmid] = "in_progress"
    return states


def derive_state(stages: dict[str, str]) -> PaperState:
    """Collapse a paper's stage rows into the one state a client renders.

    Order matters. A paper whose earlier stages succeeded and whose later one
    failed is an error, not a success — so failure is checked first. Then
    completion of `FINAL_STAGE`, then any sign of work underway; anything left
    has only pending rows, or none at all.
    """
    if not stages:
        return "queued"
    if any(status == "failed" for status in stages.values()):
        return "error"
    if stages.get(FINAL_STAGE) == "done":
        return "success"
    if any(status == "running" for status in stages.values()) or any(
        status == "done" for status in stages.values()
    ):
        return "started"
    return "queued"


def paper_progress(session: Session, pmids: Sequence[int]) -> list[PaperProgress]:
    """Per-stage state for each PMID, in the order given.

    A PMID with no `papers` row still yields an entry, with `paper_id` None and
    an empty `stages` map, so callers can tell "not started" from "not asked
    about". `error` carries the message from the first failed stage.
    """
    if not pmids:
        return []

    ids = dict(
        session.execute(select(Paper.pmid, Paper.id).where(Paper.pmid.in_(list(pmids)))).all()
    )
    progress = {pmid: PaperProgress(pmid=pmid, paper_id=ids.get(pmid)) for pmid in pmids}

    if ids:
        by_paper = {paper_id: pmid for pmid, paper_id in ids.items()}
        rows = session.execute(
            select(
                PaperStageRun.paper_id,
                PaperStageRun.stage,
                PaperStageRun.status,
                PaperStageRun.error,
            ).where(PaperStageRun.paper_id.in_(list(by_paper)))
        ).all()
        for paper_id, stage, status, error in rows:
            entry = progress[by_paper[paper_id]]
            entry.stages[stage] = status
            if status == "failed" and entry.error is None:
                entry.error = error

    for entry in progress.values():
        entry.state = derive_state(entry.stages)

    return [progress[pmid] for pmid in pmids]


# --------------------------------------------------------------- ledger writes


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
    rather than data. `attempt` increments whenever a stage starts running.
    """
    now = dt.datetime.now(dt.timezone.utc)
    values = {
        "paper_id": paper_id,
        "stage": stage,
        "status": status,
        "attempt": 1 if status == "running" else 0,
        "error": error,
        "input_fingerprint": fingerprint,
        "started_at": now if status == "running" else None,
        "finished_at": now if status in ("done", "failed", "skipped") else None,
    }
    update = {
        "status": status,
        "error": error,
        "input_fingerprint": fingerprint,
        "finished_at": values["finished_at"],
    }
    if status == "running":
        update["attempt"] = PaperStageRun.__table__.c.attempt + 1
        update["started_at"] = now

    session.execute(
        insert(PaperStageRun)
        .values(**values)
        .on_conflict_do_update(index_elements=["paper_id", "stage"], set_=update)
    )


def mark_queued(session: Session, paper_id: int, stages: Sequence[str]) -> None:
    """Mark stages 'pending' at queue time, without clobbering completed work.

    `/import` calls this before submitting the chain so the paper is visible as
    in_progress immediately. The `WHERE status <> 'done'` matters: a plain
    upsert would reset an already-finished `ingest` to pending, and the task's
    fingerprint check would then re-fetch a paper we already hold — the exact
    call to a rate-limited API this design exists to avoid.
    """
    if not stages:
        return
    session.execute(
        insert(PaperStageRun)
        .values(
            [
                {"paper_id": paper_id, "stage": stage, "status": "pending", "attempt": 0}
                for stage in stages
            ]
        )
        .on_conflict_do_update(
            index_elements=["paper_id", "stage"],
            set_={"status": "pending", "error": None, "finished_at": None},
            where=PaperStageRun.__table__.c.status != "done",
        )
    )


def stage_is_current(
    session: Session, paper_id: int, stage: str, fingerprint: Optional[str]
) -> bool:
    """True when the stage is 'done' for this fingerprint, so it can be skipped."""
    row = session.execute(
        select(PaperStageRun.status, PaperStageRun.input_fingerprint).where(
            PaperStageRun.paper_id == paper_id, PaperStageRun.stage == stage
        )
    ).first()
    if row is None:
        return False
    status, stored = row
    return status == "done" and stored == fingerprint


# ------------------------------------------------------------------ paper rows


def reserve_paper(session: Session, pmid: int) -> int:
    """Ensure a `papers` row exists for this PMID, returning its id.

    Called by `/import` before queueing, so the ledger has something to hang an
    'ingest pending' row on. Without it, a paper is invisible between queueing
    and its first task finishing, and `in_progress` cannot be detected.
    """
    session.execute(
        insert(Paper)
        .values(pmid=pmid, has_full_text=False)
        .on_conflict_do_nothing(index_elements=["pmid"])
    )
    return session.execute(select(Paper.id).where(Paper.pmid == pmid)).scalar_one()


def upsert_paper(session: Session, paper: PaperResponse) -> int:
    """Insert or update the `papers` row from a fetched paper, returning its id.

    `ON CONFLICT (pmid) DO UPDATE`, so two workers importing the same PMID
    cannot race into a constraint violation.
    """
    if paper.pmid is None:
        raise ValueError("cannot persist a paper without a PMID")

    values = {
        "pmid": paper.pmid,
        "pmcid": paper.pmcid,
        "doi": paper.doi,
        "title": paper.title,
        "journal": paper.journal,
        "journal_title": paper.journal_title,
        "pub_date": _parse_date(paper.date),
        "pub_year": _parse_year(paper.year, paper.date),
        "volume": paper.volume,
        "fpage": paper.fpage,
        "lpage": paper.lpage,
        "has_full_text": paper.has_full_text,
        "body_text": build_body_text(paper.passages),
        "fetched_at": dt.datetime.now(dt.timezone.utc),
    }
    updatable = {k: v for k, v in values.items() if k != "pmid"}
    updatable["updated_at"] = func.now()

    return session.execute(
        insert(Paper)
        .values(**values)
        .on_conflict_do_update(index_elements=["pmid"], set_=updatable)
        .returning(Paper.id)
    ).scalar_one()


def store_raw_document(session: Session, paper_id: int, raw: dict) -> None:
    """Persist the verbatim PubTator response.

    This is what lets a later stage re-run without touching the network, and
    what makes re-chunking a local operation.
    """
    session.execute(
        insert(PaperPubtatorDoc)
        .values(paper_id=paper_id, raw=raw, fetched_at=dt.datetime.now(dt.timezone.utc))
        .on_conflict_do_update(
            index_elements=["paper_id"],
            set_={"raw": raw, "fetched_at": dt.datetime.now(dt.timezone.utc)},
        )
    )


# ------------------------------------------------------------------ child rows


def replace_authors(session: Session, paper_id: int, paper: PaperResponse) -> int:
    session.execute(delete(PaperAuthor).where(PaperAuthor.paper_id == paper_id))
    rows = [
        {
            "paper_id": paper_id,
            "ordinal": index,
            "surname": author.surname,
            "given_names": author.given_names,
        }
        for index, author in enumerate(paper.authors)
    ]
    if rows:
        session.execute(insert(PaperAuthor), rows)
    return len(rows)


def replace_references(session: Session, paper_id: int, paper: PaperResponse) -> int:
    session.execute(delete(PaperReference).where(PaperReference.paper_id == paper_id))
    rows = [
        {
            "paper_id": paper_id,
            "ordinal": reference.ordinal,
            "title": reference.title,
            "ref_pmid": reference.pmid,
            "ref_doi": reference.doi,
            "source": reference.source,
            "year": reference.year,
            "volume": reference.volume,
            "fpage": reference.fpage,
            "lpage": reference.lpage,
        }
        for reference in paper.references
    ]
    if rows:
        session.execute(insert(PaperReference), rows)
    return len(rows)


#: The source database for the two entity types whose ids ever arrive bare.
#: Used only when a relation names a concept no annotation in the same paper
#: did, so there is nothing to read the database from.
TYPE_DATABASES = {"Gene": "ncbi_gene", "Species": "ncbi_taxonomy"}

#: An id that carries a namespace states its own provenance, so the prefix is a
#: valid last resort when no annotation supplied `database`. Values are the ones
#: upstream itself uses, so a concept resolved this way is indistinguishable
#: from one resolved from the annotation.
PREFIX_DATABASES = {"MESH": "ncbi_mesh", "CVCL": "cvcl", "OMIM": "omim"}


def concept_databases(paper: PaperResponse) -> dict[str, str]:
    """identifier -> source database, from this paper's annotations.

    Keyed on both the identifier as it arrived and its bare suffix, so a lookup
    succeeds whether the caller holds "ncbi_gene:672" or "672". Relation roles
    carry no database of their own, and must resolve to the same entity row the
    annotations created.
    """
    databases: dict[str, str] = {}
    for passage in paper.passages:
        for annotation in passage.annotations:
            raw = (annotation.identifier or "").strip()
            if raw and annotation.database:
                databases[raw] = annotation.database
                databases[raw.split(":", 1)[-1]] = annotation.database
                if ":" in raw:
                    # Also by namespace, so a relation-only "MESH:D999" that no
                    # annotation named still resolves from a sibling MeSH id.
                    databases.setdefault(raw.split(":", 1)[0], annotation.database)
    return databases


def source_database(
    identifier: str, kind: Optional[str], databases: dict[str, str], explicit: object = None
) -> Optional[str]:
    """Where this concept came from, or None if that cannot be established.

    In order: what upstream said outright, what a sibling annotation in the
    same paper said about this id or its namespace, the type (Gene and Species
    are the only kinds whose ids arrive bare), and finally the namespace the id
    carries. None means the concept is rejected — see `upsert_entities`.
    """
    if explicit:
        return str(explicit).strip() or None
    prefix = identifier.split(":", 1)[0] if ":" in identifier else ""
    return (
        databases.get(identifier)
        or (databases.get(prefix) if prefix else None)
        or TYPE_DATABASES.get(kind or "")
        or (PREFIX_DATABASES.get(prefix.upper()) if prefix else None)
    )


def upsert_entities(session: Session, paper: PaperResponse) -> dict[str, int]:
    """Ensure an `entities` row per grounded concept; return identifier -> id.

    Ungrounded concepts never reach this table: they fragment the graph, which
    is the whole reason for grounding entities. Relations reference concepts
    too, and may name one no annotation did.

    This is the single gate in front of `entities`, so every identifier is
    validated here rather than trusted from the parser. `normalise_identifier`
    is the same rule as the table's CHECK constraint — application-side so a
    malformed id is dropped with a warning, database-side so it cannot land at
    all if this is ever bypassed. It also qualifies a bare id with its source
    database, since "672" is unique only within NCBI Gene.

    Relations name concepts too, but a relation role carries no `database` --
    only an id, a name and a type. Unqualified, a relation-only concept would
    become a second row for something the annotations already stored as
    `ncbi_gene:672`. So the paper's annotations are read first into a lookup.

    A concept whose source database cannot be established is **rejected**: an
    entity with no provenance is not worth storing, and `entities.database` is
    NOT NULL. Rejection is per concept, never per paper -- the paper, its
    chunks and its other entities are imported regardless, and the count is
    logged.
    """
    wanted: dict[str, dict] = {}
    skipped = 0
    no_provenance = 0

    databases = concept_databases(paper)

    def remember(raw: object, name: Optional[str], kind: Optional[str], database=None) -> bool:
        nonlocal no_provenance
        text = str(raw).strip() if raw is not None else ""
        source = source_database(text, kind, databases, database)
        if source is None:
            no_provenance += 1
            return True  # counted here, not as a malformed identifier
        identifier = normalise_identifier(raw, source)
        if identifier is None:
            return False
        wanted.setdefault(
            identifier,
            {
                "identifier": identifier,
                # A blank type is as useless as a blank id, and arrived the
                # same way: entity 49 had entity_type " " alongside its " " id.
                "entity_type": (kind or "").strip() or "Unknown",
                "database": source,
                "name": name,
            },
        )
        return True

    for passage in paper.passages:
        for annotation in passage.annotations:
            if not annotation.grounded:
                continue
            if not remember(
                annotation.identifier, annotation.name, annotation.type, annotation.database
            ):
                skipped += 1
    for relation in paper.relations:
        for identifier, name, kind in (
            (relation.role1_identifier, relation.role1_name, relation.role1_type),
            (relation.role2_identifier, relation.role2_name, relation.role2_type),
        ):
            if identifier is not None and not remember(identifier, name, kind):
                skipped += 1

    if skipped:
        log.warning(
            "dropped %d concept(s) with an unusable identifier for PMID %s",
            skipped,
            paper.pmid,
        )
    if no_provenance:
        log.warning(
            "rejected %d concept(s) with no source database for PMID %s "
            "-- the paper is imported without them",
            no_provenance,
            paper.pmid,
        )

    if not wanted:
        return {}

    # Sorted by identifier, which is the conflict target. Postgres takes index
    # tuple locks in insertion order, so two transactions inserting the same
    # concepts in different orders deadlock -- and the order here was annotation
    # order, which differs per paper. Measured before the fix: papers 83 and 84
    # shared 24 inverted pairs. A total order makes a cycle impossible.
    #
    # DO UPDATE rather than DO NOTHING: RETURNING only yields rows the statement
    # actually touched, and a no-op conflict would silently drop existing ids.
    rows = session.execute(
        insert(Entity)
        .values([wanted[identifier] for identifier in sorted(wanted)])
        .on_conflict_do_update(
            index_elements=["identifier"], set_={"name": insert(Entity).excluded.name}
        )
        .returning(Entity.identifier, Entity.id)
    ).all()
    return {identifier: entity_id for identifier, entity_id in rows}


def replace_chunks(session: Session, paper_id: int, chunks: Sequence[Chunk]) -> dict[int, int]:
    """Write `paper_chunks` without vectors; return ordinal -> chunk id.

    Mentions are re-pointed at the new chunks afterwards rather than deleted:
    `chunk_id` is ON DELETE SET NULL precisely so re-chunking cannot destroy
    mention data, which is expensive to re-acquire.
    """
    session.execute(delete(PaperChunk).where(PaperChunk.paper_id == paper_id))
    if not chunks:
        return {}
    rows = session.execute(
        insert(PaperChunk)
        .values(
            [
                {
                    "paper_id": paper_id,
                    "ordinal": chunk.ordinal,
                    "section_type": chunk.section_type,
                    "chunk_type": chunk.chunk_type,
                    "char_start": chunk.char_start,
                    "char_end": chunk.char_end,
                    "text": chunk.text,
                }
                for chunk in chunks
            ]
        )
        .returning(PaperChunk.ordinal, PaperChunk.id)
    ).all()
    return {ordinal: chunk_id for ordinal, chunk_id in rows}


def replace_mentions(
    session: Session,
    paper_id: int,
    paper: PaperResponse,
    entity_ids: dict[str, int],
    chunks: Sequence[Chunk],
    chunk_ids: dict[int, int],
) -> int:
    """Write `paper_entity_mentions`, each resolved to its containing chunk."""
    session.execute(delete(PaperEntityMention).where(PaperEntityMention.paper_id == paper_id))

    rows: dict[tuple, dict] = {}
    for passage in paper.passages:
        for annotation in passage.annotations:
            identifier = normalise_identifier(annotation.identifier, annotation.database)
            if not annotation.grounded or identifier is None:
                continue
            entity_id = entity_ids.get(identifier)
            if entity_id is None:
                continue
            ordinal = find_chunk_ordinal(chunks, annotation.offset)
            key = (entity_id, annotation.offset, annotation.length)
            # uq_mention_span makes a repeated span a conflict, not a duplicate.
            rows.setdefault(
                key,
                {
                    "paper_id": paper_id,
                    "chunk_id": chunk_ids.get(ordinal) if ordinal is not None else None,
                    "entity_id": entity_id,
                    "char_offset": annotation.offset,
                    "length": annotation.length,
                    "surface_text": annotation.text,
                },
            )
    if rows:
        # Ordered by the entity they reference: inserting a mention takes a
        # KEY SHARE lock on that entity row, so an arbitrary order is the same
        # deadlock risk as the entity insert itself.
        session.execute(
            insert(PaperEntityMention), [rows[key] for key in sorted(rows)]
        )
    return len(rows)


def replace_relations(
    session: Session, paper_id: int, paper: PaperResponse, entity_ids: dict[str, int]
) -> int:
    session.execute(delete(PaperRelation).where(PaperRelation.paper_id == paper_id))

    databases = concept_databases(paper)

    def resolve(identifier: Optional[str], kind: Optional[str]) -> Optional[int]:
        """The entity id for a relation role, qualified the same way the
        entities themselves were — otherwise a gene relation would look up
        "672" against a table keyed "ncbi_gene:672" and silently vanish."""
        text = (identifier or "").strip()
        source = source_database(text, kind, databases)
        if source is None:
            return None
        return entity_ids.get(normalise_identifier(identifier, source) or "")

    rows: dict[tuple, dict] = {}
    for relation in paper.relations:
        subject = resolve(relation.role1_identifier, relation.role1_type)
        obj = resolve(relation.role2_identifier, relation.role2_type)
        if subject is None or obj is None or not relation.type:
            continue
        rows.setdefault(
            (relation.type, subject, obj),
            {
                "paper_id": paper_id,
                "relation_type": relation.type,
                "score": relation.score,
                "subject_entity_id": subject,
                "object_entity_id": obj,
            },
        )
    if rows:
        # Same reason as the mentions above: these carry FKs to `entities`.
        session.execute(insert(PaperRelation), [rows[key] for key in sorted(rows)])
    return len(rows)


def persist_paper(session: Session, paper: PaperResponse, raw: dict) -> tuple[int, int]:
    """Write a fetched paper and everything derived from it.

    Returns `(paper_id, chunk_count)`.
    """
    paper_id = upsert_paper(session, paper)
    store_raw_document(session, paper_id, raw)
    replace_authors(session, paper_id, paper)
    replace_references(session, paper_id, paper)

    entity_ids = upsert_entities(session, paper)
    chunks = chunks_from_passages(paper.passages)
    chunk_ids = replace_chunks(session, paper_id, chunks)
    replace_mentions(session, paper_id, paper, entity_ids, chunks, chunk_ids)
    replace_relations(session, paper_id, paper, entity_ids)
    return paper_id, len(chunks)


# ----------------------------------------------------------------------- utils


def _parse_date(value: Optional[str]) -> Optional[dt.date]:
    """PubTator returns e.g. '2002-04-22T00:00:00Z'."""
    if not value:
        return None
    try:
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).date()
    except ValueError:
        return None


def _parse_year(year: Optional[str], date: Optional[str]) -> Optional[int]:
    """`year` comes from the front passage and is absent on abstract-only
    responses, where the document-level date is the only source."""
    for candidate in (year, date[:4] if date else None):
        if candidate and candidate.isdigit():
            return int(candidate)
    return None
