"""Applying and inspecting the graph schema.

`schema.cypher` is the source of truth; this module only reads, splits and runs
it, then reports what the database actually holds.
"""

from __future__ import annotations

import re
from pathlib import Path
from typing import NamedTuple

from api.graph.driver import run

SCHEMA_PATH = Path(__file__).with_name("schema.cypher")

_LINE_COMMENT_RE = re.compile(r"^\s*//.*$", re.MULTILINE)


class SchemaState(NamedTuple):
    """What the database reports after `apply()` — for verification and logs."""

    constraints: list[str]
    indexes: list[str]
    labels: list[str]
    relationship_types: list[str]


def statements(path: Path = SCHEMA_PATH) -> list[str]:
    """The file's Cypher statements, comments stripped, in order."""
    text = _LINE_COMMENT_RE.sub("", path.read_text(encoding="utf-8"))
    return [statement.strip() for statement in text.split(";") if statement.strip()]


def apply(path: Path = SCHEMA_PATH) -> int:
    """Create every constraint and index. Returns the statement count.

    Each runs on its own: Neo4j takes one schema command per transaction, and
    every statement is `IF NOT EXISTS`, so a re-run is a series of no-ops.
    """
    for statement in statements(path):
        run(statement)
    return len(statements(path))


def state() -> SchemaState:
    """Read back the live schema.

    `labels` includes :Claim even though nothing populates it: creating its
    constraint registers the label token. `relationship_types` has no such
    effect available — Community cannot constrain relationships — so the
    Claim edge types are absent until something writes one.
    """
    return SchemaState(
        constraints=sorted(r["name"] for r in run("SHOW CONSTRAINTS YIELD name")),
        indexes=sorted(r["name"] for r in run("SHOW INDEXES YIELD name")),
        labels=sorted(r["label"] for r in run("CALL db.labels() YIELD label")),
        relationship_types=sorted(
            r["relationshipType"]
            for r in run("CALL db.relationshipTypes() YIELD relationshipType")
        ),
    )
