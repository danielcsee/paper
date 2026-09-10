"""`api.graph.keys` — the single place node identity is derived.

A key computed two ways eventually disagrees with itself, so these tests pin
the documented behaviour of each derivation rather than its current output.
"""

from __future__ import annotations

from typing import Optional

import pytest

from api.graph.keys import (
    ENTITY_LABEL,
    UNKNOWN_ENTITY_LABEL,
    author_key,
    entity_key,
    entity_labels,
)


def test_entity_key_passes_through_prefixed_identifier() -> None:
    """An already-namespaced identifier is returned untouched."""
    assert entity_key("MESH:D001943", "ncbi_mesh") == "MESH:D001943"
    # Idempotent: re-running over a stored value must not double-qualify.
    assert entity_key(entity_key("672", "ncbi_gene"), "ncbi_gene") == "ncbi_gene:672"
    # Surrounding whitespace is stripped before the colon test.
    assert entity_key("  CVCL:0031  ", "cvcl") == "CVCL:0031"


def test_entity_key_qualifies_bare_identifier() -> None:
    """A bare id is unique only within one NCBI database, so it gets qualified."""
    assert entity_key("672", "ncbi_gene") == "ncbi_gene:672"
    # The same number in two databases must not collapse to one row.
    assert entity_key("9606", "ncbi_gene") != entity_key("9606", "ncbi_taxonomy")
    assert entity_key("9606", " ncbi_taxonomy ") == "ncbi_taxonomy:9606"


@pytest.mark.parametrize(
    "entity_type",
    [None, "", "   ", "9606", "_leading", "Drop;MATCH (n) DETACH DELETE n", "has space"],
)
def test_entity_labels_rejects_illegal_labels(entity_type: Optional[str]) -> None:
    """Labels are interpolated into Cypher, so anything not label-shaped degrades.

    `entity_type` is upstream-controlled text with no CHECK constraint, which is
    why this is validated rather than trusted.
    """
    generic, specific = entity_labels(entity_type)
    assert generic == ENTITY_LABEL
    assert specific == UNKNOWN_ENTITY_LABEL


@pytest.mark.parametrize("entity_type", ["Gene", "Disease", "CellLine", "A", "X_9"])
def test_entity_labels_keeps_legal_labels(entity_type: str) -> None:
    """A label-shaped type is preserved as the second, typed label."""
    assert entity_labels(entity_type) == (ENTITY_LABEL, entity_type)
    assert entity_labels(f"  {entity_type}  ")[1] == entity_type


def test_author_key_normalises_case_and_whitespace() -> None:
    """Normalisation is limited to case and whitespace, by design."""
    assert author_key("German", "Alexander J.") == "german, alexander j."
    assert author_key("  GERMAN ", "Alexander   J.") == "german, alexander j."


def test_author_key_does_not_merge_initials() -> None:
    """Collapsing initials would merge different people more often than it joins one."""
    assert author_key("German", "Alexander J.") != author_key("German", "A. J.")


@pytest.mark.parametrize(
    ("surname", "given", "expected"),
    [
        ("German", None, "german"),
        (None, "Alexander", "alexander"),
        ("German", "", "german"),
        (None, None, None),
        ("", "", None),
        ("   ", None, None),
    ],
)
def test_author_key_handles_nullable_columns(
    surname: Optional[str], given: Optional[str], expected: Optional[str]
) -> None:
    """Both `paper_authors` name columns are nullable; None means unkeyable."""
    assert author_key(surname, given) == expected


def test_entity_key_contract() -> None:
    """Both derivation paths in one coverage context.

    Paired with the focused tests above: those isolate a failure to one input,
    this one is the stable target for Testledger's symbol-to-test link.
    """
    cases: list[tuple[str, str, str]] = [
        ("MESH:D001943", "ncbi_mesh", "MESH:D001943"),
        ("  CVCL:0031  ", "cvcl", "CVCL:0031"),
        ("672", "ncbi_gene", "ncbi_gene:672"),
        ("9606", " ncbi_taxonomy ", "ncbi_taxonomy:9606"),
    ]

    for identifier, database, expected in cases:
        assert entity_key(identifier, database) == expected, f"{identifier!r}/{database!r}"


def test_entity_labels_contract() -> None:
    """Legal and illegal label shapes in one coverage context."""
    for legal in ("Gene", "Disease", "X_9"):
        assert entity_labels(legal) == (ENTITY_LABEL, legal)
    for illegal in (None, "", "9606", "Drop;MATCH (n) DETACH DELETE n"):
        assert entity_labels(illegal) == (ENTITY_LABEL, UNKNOWN_ENTITY_LABEL), illegal


def test_author_key_contract() -> None:
    """Both name halves, each alone, neither, and the normalisation rules."""
    cases: list[tuple[Optional[str], Optional[str], Optional[str]]] = [
        ("German", "Alexander J.", "german, alexander j."),
        ("  GERMAN ", "Alexander   J.", "german, alexander j."),
        ("German", None, "german"),
        (None, "Alexander", "alexander"),
        (None, None, None),
        ("   ", "", None),
    ]

    for surname, given, expected in cases:
        assert author_key(surname, given) == expected, f"{surname!r}/{given!r}"
