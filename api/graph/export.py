"""Projecting the Postgres corpus into the graph.

One function per node or edge type, each a SQL read followed by a batched
`UNWIND`. They are ordered by dependency and must be run in that order:
`load_all` projects the whole corpus; `load_paper` projects one, for the
`graph` ingest stage. Both drive the same functions — a `pmid` argument narrows
each read, so there is one Cypher statement per node and edge type rather than
a bulk copy and a per-paper copy that drift apart.

Every write is a `MERGE` on the node's identity key, so re-running is a no-op
rather than a duplicate — the same property `persist_paper` already has on the
Postgres side. Edges `MATCH` their endpoints instead of merging them: a missing
endpoint means the node load was wrong, and it should fail loudly here rather
than leave a propertyless stub behind to be discovered later.

`:Claim` and its edges (`ASSERTS`, `SUBJECT`, `OBJECT`, `CONTRADICTS`) are
deliberately absent — how claims are built is still undecided. Adding them is
purely additive and needs no migration.
"""

from __future__ import annotations

import logging
from collections import defaultdict
from typing import Any, NamedTuple, Optional

from sqlalchemy import text
from sqlalchemy.orm import Session

from api.graph.driver import run_batched
from api.graph.keys import author_key, entity_key, entity_labels

log = logging.getLogger(__name__)


class LoadCounts(NamedTuple):
    """Rows written per step, in load order."""

    entities: int
    papers: int
    external_papers: int
    authors: int
    authored: int
    cites: int
    mentions: int


def load_all(session: Session, pmid: Optional[int] = None) -> LoadCounts:
    """Project the corpus, or one paper of it. Idempotent; safe to re-run.

    The order is a dependency order, not a preference: edges `MATCH` their
    endpoints, so every node they touch must exist first. A single paper needs
    its own external reference stubs loaded before `CITES` can attach to them.
    """
    counts = LoadCounts(
        entities=load_entities(session, pmid),
        papers=load_papers(session, pmid),
        external_papers=load_external_papers(session, pmid),
        authors=load_authors(session, pmid),
        authored=load_authored(session, pmid),
        cites=load_cites(session, pmid),
        mentions=load_mentions(session, pmid),
    )
    log.info("graph load complete (%s): %s",
             f"pmid {pmid}" if pmid else "whole corpus", counts._asdict())
    return counts


def load_paper(session: Session, pmid: int) -> LoadCounts:
    """Project one paper and everything it touches. The `graph` ingest stage."""
    return load_all(session, pmid)


def _params(pmid: Optional[int]) -> dict[str, Any]:
    """The filter bind. Every use is CAST — Postgres cannot infer the type of a
    NULL parameter, and `WHERE :pmid IS NULL` is exactly that case."""
    return {"pmid": pmid}


# --------------------------------------------------------------------- nodes


def load_entities(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Entity:<Type> {entity_id})`, one per `entities` row.

    Grouped by type because a label cannot be parameterised in Cypher — it is
    interpolated, so it is validated first by `entity_labels`.

    Narrowed to the concepts one paper mentions when `pmid` is given. Entities
    are shared, so this re-`MERGE`s ones other papers already created; that is
    the point of merging on the key.
    """
    rows = session.execute(
        text(
            """
            SELECT e.identifier, e.entity_type, e.database, e.name
            FROM entities e
            WHERE CAST(:pmid AS bigint) IS NULL OR e.id IN (
                SELECT m.entity_id FROM paper_entity_mentions m
                JOIN papers p ON p.id = m.paper_id
                WHERE p.pmid = CAST(:pmid AS bigint)
            )
            ORDER BY e.identifier
            """
        ),
        _params(pmid),
    ).mappings()

    by_label: dict[tuple[str, str], list[dict]] = defaultdict(list)
    for row in rows:
        labels = entity_labels(row["entity_type"])
        by_label[labels].append(
            {
                "entity_id": entity_key(row["identifier"], row["database"]),
                "identifier": row["identifier"],
                "entity_type": row["entity_type"],
                "database": row["database"],
                "name": row["name"],
            }
        )

    total = 0
    for (generic, specific), batch in sorted(by_label.items()):
        total += run_batched(
            f"""
            UNWIND $rows AS row
            MERGE (e:{generic} {{entity_id: row.entity_id}})
            SET e:{specific},
                e.identifier  = row.identifier,
                e.entity_type = row.entity_type,
                e.database    = row.database,
                e.name        = row.name
            """,
            batch,
        )
    return total


def load_papers(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Paper {pmid, in_corpus: true})` for every imported paper."""
    rows = [
        dict(row)
        for row in session.execute(
            text(
                """
                SELECT pmid, pmcid, doi, title, journal, journal_title,
                       pub_date, pub_year, volume, has_full_text
                FROM papers
                WHERE CAST(:pmid AS bigint) IS NULL OR pmid = :pmid
                ORDER BY pmid
                """
            ),
            _params(pmid),
        ).mappings()
    ]
    return run_batched(
        """
        UNWIND $rows AS row
        MERGE (p:Paper {pmid: row.pmid})
        SET p.in_corpus     = true,
            p.pmcid         = row.pmcid,
            p.doi           = row.doi,
            p.title         = row.title,
            p.journal       = row.journal,
            p.journal_title = row.journal_title,
            p.pub_date      = row.pub_date,
            p.pub_year      = row.pub_year,
            p.volume        = row.volume,
            p.has_full_text = row.has_full_text
        """,
        rows,
    )


def load_external_papers(session: Session, pmid: Optional[int] = None) -> int:
    """Stub `(:Paper {in_corpus: false})` for cited works we do not hold.

    Without these the citation structure is almost empty — the corpus barely
    cites itself, and the signal lives in *shared* external references. Their
    properties come from the bibliography entry, so they are sparse by nature.

    `ON CREATE` on the payload, not `SET`: an external stub must never overwrite
    a real paper's fields if the same PMID is imported later.
    """
    rows = [
        dict(row)
        for row in session.execute(
            text(
                """
                SELECT r.ref_pmid::bigint          AS pmid,
                       MIN(r.title)                AS title,
                       MIN(r.source)               AS journal,
                       MIN(NULLIF(r.year, ''))     AS year
                FROM paper_references r
                WHERE r.ref_pmid ~ '^[0-9]+$'
                  AND NOT EXISTS (
                      SELECT 1 FROM papers p WHERE p.pmid = r.ref_pmid::bigint
                  )
                  AND (CAST(:pmid AS bigint) IS NULL OR r.paper_id = (
                      SELECT id FROM papers WHERE pmid = CAST(:pmid AS bigint)
                  ))
                GROUP BY r.ref_pmid::bigint
                ORDER BY 1
                """
            ),
            _params(pmid),
        ).mappings()
    ]
    return run_batched(
        """
        UNWIND $rows AS row
        MERGE (p:Paper {pmid: row.pmid})
        ON CREATE SET p.in_corpus = false,
                      p.title     = row.title,
                      p.journal   = row.journal,
                      p.pub_year  = toInteger(row.year)
        """,
        rows,
    )


def load_authors(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Author {author_id})`, deduplicated by name across the corpus.

    Display names come from the lowest `(surname, given_names)` in sort order so
    a re-run picks the same spelling; `author_id` is casefolded, the properties
    are not.
    """
    rows = session.execute(
        text(
            """
            SELECT DISTINCT a.surname, a.given_names
            FROM paper_authors a
            JOIN papers p ON p.id = a.paper_id
            WHERE CAST(:pmid AS bigint) IS NULL OR p.pmid = :pmid
            ORDER BY a.surname, a.given_names
            """
        ),
        _params(pmid),
    ).mappings()

    by_key: dict[str, dict] = {}
    for row in rows:
        key = author_key(row["surname"], row["given_names"])
        if key is None:
            continue
        by_key.setdefault(
            key,
            {
                "author_id": key,
                "surname": row["surname"],
                "given_names": row["given_names"],
            },
        )

    return run_batched(
        """
        UNWIND $rows AS row
        MERGE (a:Author {author_id: row.author_id})
        SET a.surname     = row.surname,
            a.given_names = row.given_names
        """,
        list(by_key.values()),
    )


# --------------------------------------------------------------------- edges


def load_authored(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Author)-[:AUTHORED {ordinal}]->(:Paper)`.

    `ordinal` is the author's position on that paper: first and last authorship
    carry meaning in biomedical convention, so it is worth keeping on the edge.
    """
    rows = [
        dict(row)
        for row in session.execute(
            text(
                """
                SELECT p.pmid, a.surname, a.given_names, a.ordinal
                FROM paper_authors a
                JOIN papers p ON p.id = a.paper_id
                WHERE CAST(:pmid AS bigint) IS NULL OR p.pmid = CAST(:pmid AS bigint)
                ORDER BY p.pmid, a.ordinal
                """
            ),
            _params(pmid),
        ).mappings()
    ]
    edges = []
    for row in rows:
        key = author_key(row["surname"], row["given_names"])
        if key is None:
            continue
        edges.append({"author_id": key, "pmid": row["pmid"], "ordinal": row["ordinal"]})

    return run_batched(
        """
        UNWIND $rows AS row
        MATCH (a:Author {author_id: row.author_id})
        MATCH (p:Paper {pmid: row.pmid})
        MERGE (a)-[r:AUTHORED]->(p)
        SET r.ordinal = row.ordinal
        """,
        edges,
    )


def load_cites(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Paper)-[:CITES]->(:Paper)`, for references that resolve to a PMID.

    Targets are mostly the external stubs from `load_external_papers`; only a
    handful of references point inside the corpus.
    """
    rows = [
        dict(row)
        for row in session.execute(
            text(
                """
                SELECT DISTINCT p.pmid AS source_pmid,
                                r.ref_pmid::bigint AS target_pmid
                FROM paper_references r
                JOIN papers p ON p.id = r.paper_id
                WHERE r.ref_pmid ~ '^[0-9]+$'
                  AND (CAST(:pmid AS bigint) IS NULL OR p.pmid = CAST(:pmid AS bigint))
                ORDER BY 1, 2
                """
            ),
            _params(pmid),
        ).mappings()
    ]
    return run_batched(
        """
        UNWIND $rows AS row
        MATCH (source:Paper {pmid: row.source_pmid})
        MATCH (target:Paper {pmid: row.target_pmid})
        MERGE (source)-[:CITES]->(target)
        """,
        rows,
    )


def load_mentions(session: Session, pmid: Optional[int] = None) -> int:
    """`(:Paper)-[:MENTIONS {mention_count, sections}]->(:Entity)`.

    An aggregation, not a row-per-row copy: thousands of mention spans collapse
    to one edge per (paper, entity) pair carrying how often and where.

    `sections` is kept rather than filtered, so boilerplate mentions — ABBR,
    ACK_FUND, COMP_INT — can be excluded at query time without a re-export. The
    IDF weight that keeps hub concepts from dominating PageRank is deliberately
    not computed yet: at this corpus size it would be noise, and adding it later
    is a `SET` over existing edges.
    """
    rows = [
        dict(row)
        for row in session.execute(
            text(
                """
                SELECT p.pmid,
                       e.identifier,
                       e.database,
                       COUNT(*) AS mention_count,
                       COALESCE(
                           ARRAY_AGG(DISTINCT c.section_type)
                               FILTER (WHERE c.section_type IS NOT NULL),
                           '{}'
                       ) AS sections
                FROM paper_entity_mentions m
                JOIN papers p   ON p.id = m.paper_id
                JOIN entities e ON e.id = m.entity_id
                LEFT JOIN paper_chunks c ON c.id = m.chunk_id
                WHERE CAST(:pmid AS bigint) IS NULL OR p.pmid = CAST(:pmid AS bigint)
                GROUP BY p.pmid, e.identifier, e.database
                ORDER BY p.pmid, e.identifier
                """
            ),
            _params(pmid),
        ).mappings()
    ]
    edges = [
        {
            "pmid": row["pmid"],
            "entity_id": entity_key(row["identifier"], row["database"]),
            "mention_count": row["mention_count"],
            "sections": sorted(row["sections"]),
        }
        for row in rows
    ]
    return run_batched(
        """
        UNWIND $rows AS row
        MATCH (p:Paper {pmid: row.pmid})
        MATCH (e:Entity {entity_id: row.entity_id})
        MERGE (p)-[r:MENTIONS]->(e)
        SET r.mention_count = row.mention_count,
            r.sections      = row.sections
        """,
        edges,
    )
