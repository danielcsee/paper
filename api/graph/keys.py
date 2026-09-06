"""Graph identity keys, derived from Postgres rows.

Every node key is computed here and nowhere else. Two consumers need them —
the bulk export and, later, the per-paper `graph` ingest stage — and a key
derived two ways is a key that eventually disagrees with itself.
"""

from __future__ import annotations

import re
from typing import Optional

#: A label is interpolated into Cypher (labels cannot be parameterised), so it
#: is validated rather than trusted. `entity_type` is upstream-controlled text
#: with no CHECK constraint, by deliberate design in `api.db.models`.
LABEL_RE = re.compile(r"^[A-Za-z][A-Za-z0-9_]*$")

#: Every `:Entity` also carries its type as a second label, so typed queries
#: and future per-type constraints have something to bind to.
ENTITY_LABEL = "Entity"

#: Used when `entity_type` is missing or is not a legal label. `upsert_entities`
#: already writes "Unknown" for a blank type; this covers anything else.
UNKNOWN_ENTITY_LABEL = "Unknown"

_WHITESPACE_RE = re.compile(r"\s+")


def entity_key(identifier: str, database: str) -> str:
    """The `:Entity.entity_id` for one `entities` row.

    Identifiers are already namespaced by ingest — `MESH:D001943` arrives that
    way, `672` is qualified to `ncbi_gene:672` — so this is a pass-through in
    practice. The fallback qualifies a bare id anyway, because the table's
    CHECK constraint still admits one and a bare number is unique only within a
    single NCBI database.
    """
    identifier = identifier.strip()
    if ":" in identifier:
        return identifier
    return f"{database.strip()}:{identifier}"


def entity_labels(entity_type: Optional[str]) -> tuple[str, str]:
    """`(:Entity, :<Type>)` — the generic label for traversal, the specific one
    for typed queries."""
    specific = (entity_type or "").strip()
    if not LABEL_RE.match(specific):
        specific = UNKNOWN_ENTITY_LABEL
    return ENTITY_LABEL, specific


def author_key(surname: Optional[str], given_names: Optional[str]) -> Optional[str]:
    """The `:Author.author_id` for one `paper_authors` row.

    A placeholder, and deliberately a weak one: `paper_authors` holds no ORCID,
    no affiliation and no email, so a name is the only thing there is to key on.
    Normalisation is limited to case and whitespace — "German, Alexander J." and
    "German, A. J." stay two authors, because collapsing initials would merge
    genuinely different people far more often than it would join one.

    Returns None when there is no name at all; both columns are nullable.
    """
    surname = _normalise(surname)
    given = _normalise(given_names)
    if surname and given:
        return f"{surname}, {given}"
    return surname or given or None


def _normalise(value: Optional[str]) -> str:
    """Casefold and collapse whitespace. Empty string when there is nothing."""
    if value is None:
        return ""
    return _WHITESPACE_RE.sub(" ", value).strip().casefold()
