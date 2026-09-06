"""The paper schema.

Design rules this encodes, both settled earlier by measurement:

* Store what cannot be recomputed locally — authors, references, annotations and
  relations all come from a rate-limited external API, so re-deriving them means
  re-fetching. A paper-level SPECTER embedding is deliberately absent: it is
  rebuildable from text we already hold, so it can be added by migration later
  at no cost.
* `pmid` is the natural key, not `pmcid`. PubTator is keyed on PMID and search
  always returns one; `pmcid` is null for ~25% of results, which are
  abstract-only. Those papers still get a row, with `has_full_text = false`.

Upstream-controlled vocabularies (`section_type`, `entity_type`,
`relation_type`) are plain text with no CHECK constraint — NCBI can add values
and a constraint would turn that into an ingest failure. Vocabularies we own
(`stage`, `status`) are constrained.
"""

from __future__ import annotations

import datetime as dt
from typing import Optional

from pgvector.sqlalchemy import Vector
from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    Date,
    DateTime,
    ForeignKey,
    Index,
    Integer,
    Numeric,
    String,
    Text,
    UniqueConstraint,
    func,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Mapped, mapped_column, relationship

from api.db.base import Base

#: bge-base-en-v1.5 / e5-base. Changing this needs a migration and a re-embed.
EMBEDDING_DIM = 768


class Paper(Base):
    __tablename__ = "papers"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    pmid: Mapped[int] = mapped_column(BigInteger, unique=True, nullable=False)
    #: Null means the paper is not in PMC, so PubTator has abstract text only.
    pmcid: Mapped[Optional[str]] = mapped_column(String(32), unique=True)

    doi: Mapped[Optional[str]] = mapped_column(Text)
    title: Mapped[Optional[str]] = mapped_column(Text)
    #: NLM abbreviation, e.g. "BMC Musculoskelet Disord".
    journal: Mapped[Optional[str]] = mapped_column(Text)
    #: Full title from the front passage, e.g. "BMC Musculoskeletal Disorders".
    journal_title: Mapped[Optional[str]] = mapped_column(Text)
    pub_date: Mapped[Optional[dt.date]] = mapped_column(Date)
    pub_year: Mapped[Optional[int]] = mapped_column(Integer)
    volume: Mapped[Optional[str]] = mapped_column(Text)
    fpage: Mapped[Optional[str]] = mapped_column(Text)
    lpage: Mapped[Optional[str]] = mapped_column(Text)

    has_full_text: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)
    #: PubTator's reconstructed document text. Chunk and mention offsets index
    #: into this, so it is what makes them resolvable for display/highlighting.
    body_text: Mapped[Optional[str]] = mapped_column(Text)

    fetched_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    updated_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now(), nullable=False
    )

    authors: Mapped[list["PaperAuthor"]] = relationship(
        back_populates="paper", cascade="all, delete-orphan"
    )
    chunks: Mapped[list["PaperChunk"]] = relationship(
        back_populates="paper", cascade="all, delete-orphan"
    )
    mentions: Mapped[list["PaperEntityMention"]] = relationship(
        back_populates="paper", cascade="all, delete-orphan"
    )
    relations: Mapped[list["PaperRelation"]] = relationship(
        back_populates="paper", cascade="all, delete-orphan"
    )
    references: Mapped[list["PaperReference"]] = relationship(
        back_populates="paper", cascade="all, delete-orphan"
    )

    __table_args__ = (Index("ix_papers_pub_year", "pub_year"),)


class PaperPubtatorDoc(Base):
    """The verbatim PubTator response.

    Kept because we persist *chunks*, not passages: re-chunking with different
    packing rules needs the passages back, and without this that means 888
    papers through a rate-limited API again. It is also lossless, where our
    normalisation drops fields (`accession`, `valid`, `nodes`, …).
    """

    __tablename__ = "paper_pubtator_docs"

    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), primary_key=True
    )
    fetched_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    raw: Mapped[dict] = mapped_column(JSONB, nullable=False)


class PaperAuthor(Base):
    __tablename__ = "paper_authors"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), nullable=False
    )
    ordinal: Mapped[int] = mapped_column(Integer, nullable=False)
    surname: Mapped[Optional[str]] = mapped_column(Text)
    given_names: Mapped[Optional[str]] = mapped_column(Text)

    paper: Mapped[Paper] = relationship(back_populates="authors")

    __table_args__ = (
        UniqueConstraint("paper_id", "ordinal", name="uq_paper_authors_paper_ordinal"),
        Index("ix_paper_authors_surname", "surname"),
    )


class PaperChunk(Base):
    """A packed run of PubTator passages: the retrieval unit."""

    __tablename__ = "paper_chunks"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), nullable=False
    )
    ordinal: Mapped[int] = mapped_column(Integer, nullable=False)
    section_type: Mapped[Optional[str]] = mapped_column(String(32))
    #: PubTator's finer-grained passage kind: front, abstract_title_1, title_1,
    #: title_2, paragraph, table_caption, ... This is what separates a section
    #: heading from body text, so a reader can render the paper as a document.
    chunk_type: Mapped[Optional[str]] = mapped_column(String(32))
    #: Half-open span in PubTator's document coordinate space. A mention belongs
    #: to this chunk when char_start <= mention.char_offset < char_end.
    char_start: Mapped[int] = mapped_column(Integer, nullable=False)
    char_end: Mapped[int] = mapped_column(Integer, nullable=False)
    text: Mapped[str] = mapped_column(Text, nullable=False)
    embedding: Mapped[Optional[list[float]]] = mapped_column(Vector(EMBEDDING_DIM))

    paper: Mapped[Paper] = relationship(back_populates="chunks")

    __table_args__ = (
        UniqueConstraint("paper_id", "ordinal", name="uq_paper_chunks_paper_ordinal"),
        CheckConstraint("char_end > char_start", name="ck_paper_chunks_span"),
        Index("ix_paper_chunks_paper_span", "paper_id", "char_start", "char_end"),
        Index("ix_paper_chunks_section_type", "section_type"),
        # Declared here, not just in the migration, so autogenerate knows it
        # exists and stops emitting a DROP for it on every future revision.
        Index(
            "ix_paper_chunks_embedding_hnsw",
            "embedding",
            postgresql_using="hnsw",
            postgresql_ops={"embedding": "vector_cosine_ops"},
        ),
    )


class Entity(Base):
    """A grounded concept, deduplicated across the corpus.

    Identity is the ontology id, never the surface text — that is the whole
    point of using PubTator rather than LLM-extracted entity names.
    Ungrounded annotations (upstream identifier "-") are dropped at ingest and
    never reach this table.
    """

    __tablename__ = "entities"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    #: Namespaced concept id: "MESH:D002118", "ncbi_gene:672", "CVCL:0031", …
    #: Ids that arrive bare are qualified with their source database at ingest,
    #: because a bare number is unique only within one NCBI database — gene
    #: 9606 and taxon 9606 are different concepts and this column is unique.
    identifier: Mapped[str] = mapped_column(Text, unique=True, nullable=False)
    #: Gene | Disease | Chemical | Species | CellLine | Variant | Chromosome
    entity_type: Mapped[str] = mapped_column(String(32), nullable=False)
    database: Mapped[Optional[str]] = mapped_column(String(64))
    name: Mapped[Optional[str]] = mapped_column(Text)

    #: The two shapes NCBI actually produces. Kept in sync with
    #: `api.pb_client.models.IDENTIFIER_RE`, which drops a bad id at ingest;
    #: this is the backstop for anything that bypasses that path.
    #:
    #: The suffix is alphanumeric, not numeric — MeSH ids are a letter and
    #: digits ("MESH:D065627") and are the bulk of the corpus. Ids that arrive
    #: bare are qualified with their source database at ingest
    #: ("672" -> "ncbi_gene:672"), so the prefix must admit underscores. A bare
    #: number stays legal for the case where that database is unknown.
    IDENTIFIER_SQL_RE = r"^([0-9]+|[A-Za-z][A-Za-z0-9_]*:[A-Za-z0-9._\-]+)$"

    __table_args__ = (
        CheckConstraint(
            f"identifier ~ '{IDENTIFIER_SQL_RE}'",
            name="ck_entities_identifier_format",
        ),
        Index("ix_entities_entity_type", "entity_type"),
        Index("ix_entities_name", "name"),
    )


class PaperEntityMention(Base):
    """One entity occurrence in one paper — the source of `[:MENTIONS]`."""

    __tablename__ = "paper_entity_mentions"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), nullable=False
    )
    #: Resolved from the offset once chunks exist. SET NULL rather than CASCADE:
    #: re-chunking must not destroy mentions, which are the expensive data.
    chunk_id: Mapped[Optional[int]] = mapped_column(
        BigInteger, ForeignKey("paper_chunks.id", ondelete="SET NULL")
    )
    entity_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("entities.id", ondelete="CASCADE"), nullable=False
    )
    char_offset: Mapped[int] = mapped_column(Integer, nullable=False)
    length: Mapped[int] = mapped_column(Integer, nullable=False)
    surface_text: Mapped[Optional[str]] = mapped_column(Text)

    paper: Mapped[Paper] = relationship(back_populates="mentions")
    entity: Mapped[Entity] = relationship()

    __table_args__ = (
        # Makes re-ingesting a paper idempotent.
        UniqueConstraint(
            "paper_id", "entity_id", "char_offset", "length", name="uq_mention_span"
        ),
        Index("ix_mentions_paper", "paper_id"),
        Index("ix_mentions_entity", "entity_id"),
        Index("ix_mentions_chunk", "chunk_id"),
    )


class PaperRelation(Base):
    """A document-level assertion between two concepts, with polarity.

    Opposite-polarity relations over the same (subject, object) pair in two
    different papers are the candidate CONTRADICTS edges.
    """

    __tablename__ = "paper_relations"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), nullable=False
    )
    #: Association | Positive_Correlation | Negative_Correlation | Cotreatment | Bind
    relation_type: Mapped[str] = mapped_column(String(64), nullable=False)
    score: Mapped[Optional[float]] = mapped_column(Numeric(6, 4))
    subject_entity_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("entities.id", ondelete="CASCADE"), nullable=False
    )
    object_entity_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("entities.id", ondelete="CASCADE"), nullable=False
    )

    paper: Mapped[Paper] = relationship(back_populates="relations")

    __table_args__ = (
        UniqueConstraint(
            "paper_id",
            "relation_type",
            "subject_entity_id",
            "object_entity_id",
            name="uq_paper_relation",
        ),
        # The contradiction query pivots on the concept pair across papers.
        Index("ix_relations_pair", "subject_entity_id", "object_entity_id", "relation_type"),
        Index("ix_relations_paper", "paper_id"),
    )


class PaperReference(Base):
    """A bibliography entry, from a REF passage's infons.

    `ref_pmid` is the CITES target. Measured on this corpus: ~77% of references
    resolve to a PMID, but only 0.16% point at another paper we hold — so these
    rows are mainly for co-citation structure via external stub nodes.
    """

    __tablename__ = "paper_references"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), nullable=False
    )
    ordinal: Mapped[int] = mapped_column(Integer, nullable=False)
    title: Mapped[Optional[str]] = mapped_column(Text)
    ref_pmid: Mapped[Optional[str]] = mapped_column(String(32))
    ref_doi: Mapped[Optional[str]] = mapped_column(Text)
    source: Mapped[Optional[str]] = mapped_column(Text)
    year: Mapped[Optional[str]] = mapped_column(String(16))
    volume: Mapped[Optional[str]] = mapped_column(String(32))
    fpage: Mapped[Optional[str]] = mapped_column(String(32))
    lpage: Mapped[Optional[str]] = mapped_column(String(32))

    paper: Mapped[Paper] = relationship(back_populates="references")

    __table_args__ = (
        UniqueConstraint("paper_id", "ordinal", name="uq_paper_references_paper_ordinal"),
        # Co-citation: "which papers cite this same external work?"
        Index("ix_paper_references_ref_pmid", "ref_pmid"),
    )


class PaperStageRun(Base):
    """Per-paper, per-stage ingestion ledger.

    A row is written `pending` before any external call — `/import` writes the
    `ingest` row at queue time — so an outage costs delay rather than data, and
    a paper already in flight is visible before its first task runs.

    `input_fingerprint` covers the stage's config (embedding model and
    dimension, parser version), so changing that config self-invalidates the
    stage across every paper without hand-tracking.
    """

    __tablename__ = "paper_stage_runs"

    #: One entry per Celery task in the chain, plus `graph` for the Neo4j step
    #: that does not exist yet — allowed now so adding it needs no migration.
    STAGES = ("ingest", "embed", "graph")
    STATUSES = ("pending", "running", "done", "failed", "skipped")

    #: Completing this stage is what "successfully imported" means. Lives here
    #: rather than in one package because ingestion writes it and corpus reads
    #: it, and the two must not drift. Moves to 'graph' when that stage exists.
    FINAL_STAGE = "embed"

    paper_id: Mapped[int] = mapped_column(
        BigInteger, ForeignKey("papers.id", ondelete="CASCADE"), primary_key=True
    )
    stage: Mapped[str] = mapped_column(String(32), primary_key=True)
    status: Mapped[str] = mapped_column(String(16), nullable=False, default="pending")
    attempt: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    started_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))
    finished_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))
    error: Mapped[Optional[str]] = mapped_column(Text)
    input_fingerprint: Mapped[Optional[str]] = mapped_column(String(64))
    code_version: Mapped[Optional[str]] = mapped_column(String(64))

    __table_args__ = (
        CheckConstraint(
            "stage in ('ingest','embed','graph')",
            name="ck_stage_runs_stage",
        ),
        CheckConstraint(
            "status in ('pending','running','done','failed','skipped')",
            name="ck_stage_runs_status",
        ),
        # "find me work": the scheduler's only query shape.
        Index("ix_stage_runs_stage_status", "stage", "status"),
    )
