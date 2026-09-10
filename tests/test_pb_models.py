"""`api.pb_client.models` — normalising PubTator's response shapes.

`normalise_identifier` is what keeps `entities.identifier` matching its CHECK
constraint, and its idempotence is what keeps re-running ingest from producing
`ncbi_gene:ncbi_gene:672`.
"""

from __future__ import annotations

from typing import Optional

import pytest

from api.pb_client.models import Author, PaperResponse, normalise_identifier


@pytest.mark.parametrize(
    "identifier",
    ["MESH:D065627", "CVCL:M023", "ncbi_gene:672", "OMIM:114480", "MESH:D002118"],
)
def test_normalise_identifier_passes_through_prefixed_ids(identifier: str) -> None:
    """Prefixed ids are already canonical, whatever the database argument says."""
    assert normalise_identifier(identifier, "ncbi_mesh") == identifier
    assert normalise_identifier(identifier) == identifier


def test_normalise_identifier_qualifies_bare_ids() -> None:
    """Gene 9606 and taxon 9606 are different concepts sharing a unique column."""
    assert normalise_identifier("672", "ncbi_gene") == "ncbi_gene:672"
    assert normalise_identifier(
        "9606", "ncbi_taxonomy"
    ) != normalise_identifier("9606", "ncbi_gene")


def test_normalise_identifier_is_idempotent() -> None:
    """Re-running over stored values must not double-prefix."""
    once = normalise_identifier("672", "ncbi_gene")
    assert normalise_identifier(once, "ncbi_gene") == once


def test_normalise_identifier_accepts_non_string_input() -> None:
    """Upstream types the bare form inconsistently — int for taxonomy."""
    assert normalise_identifier(9606, "ncbi_taxonomy") == "ncbi_taxonomy:9606"


def test_normalise_identifier_strips_surrounding_whitespace() -> None:
    assert normalise_identifier("  672  ", "  ncbi_gene  ") == "ncbi_gene:672"


@pytest.mark.parametrize(
    "value", [None, "-", "", "   ", "not an id", "MESH:", ":D002118", "672:", "a b:c"]
)
def test_normalise_identifier_returns_none_for_ungrounded_values(
    value: Optional[object],
) -> None:
    """Every way upstream declines to ground a mention collapses to None."""
    assert normalise_identifier(value, "ncbi_mesh") is None


@pytest.mark.parametrize("database", [None, "", "   ", "9bad", "has space", "-"])
def test_normalise_identifier_keeps_bare_id_when_database_is_unusable(
    database: Optional[str],
) -> None:
    """Unqualifiable: keep the bare id rather than drop it or invent a prefix."""
    assert normalise_identifier("672", database) == "672"


@pytest.mark.parametrize(
    ("surname", "given", "expected"),
    [
        ("German", "Alexander J.", "German Alexander J."),
        ("German", None, "German"),
        (None, "Alexander", "Alexander"),
        (None, None, ""),
    ],
)
def test_author_display_joins_present_halves_only(
    surname: Optional[str], given: Optional[str], expected: str
) -> None:
    """A missing half must not leave a stray separator."""
    assert Author(surname=surname, given_names=given).display == expected


@pytest.mark.parametrize(
    ("pmcid", "has_full_text", "expected"),
    [
        ("PMC1234567", True, True),
        ("PMC1234567", False, False),
        (None, True, False),
        (None, False, False),
        ("", True, False),
    ],
)
def test_paper_importable_requires_both_identity_and_full_text(
    pmcid: Optional[str], has_full_text: bool, expected: bool
) -> None:
    """`pmcid` alone says nothing about a light response; `has_full_text` alone
    admits body text with no PMC identity to import against."""
    paper = PaperResponse(pmcid=pmcid, has_full_text=has_full_text)

    assert paper.importable is expected


def test_paper_importable_is_not_has_passages() -> None:
    """An abstract-only record comes back with two passages and is not importable."""
    from api.pb_client.models import Passage

    paper = PaperResponse(
        pmcid=None,
        has_full_text=False,
        passages=[
            Passage(ordinal=0, offset=0, text="Title"),
            Passage(ordinal=1, offset=10, text="Abstract"),
        ],
    )

    assert paper.passages
    assert paper.importable is False


def test_normalise_identifier_contract() -> None:
    """Every documented shape in one coverage context.

    The parametrized tests above isolate failures case by case; this walks the
    whole contract in a single test, which is what Testledger attributes to the
    function (its coverage threshold is per-test, not unioned across the file).
    """
    cases: list[tuple[object, Optional[str], Optional[str]]] = [
        ("MESH:D065627", "ncbi_mesh", "MESH:D065627"),   # prefixed passthrough
        ("ncbi_gene:672", "ncbi_gene", "ncbi_gene:672"),  # idempotent
        ("672", "ncbi_gene", "ncbi_gene:672"),            # qualified
        (9606, "ncbi_taxonomy", "ncbi_taxonomy:9606"),    # non-string input
        ("  672  ", "  ncbi_gene  ", "ncbi_gene:672"),    # stripped
        ("672", None, "672"),                             # unqualifiable
        ("672", "9bad", "672"),                           # illegal prefix
        (None, "ncbi_mesh", None),                        # absent
        ("-", "ncbi_mesh", None),                         # upstream's null
        ("", "ncbi_mesh", None),                          # empty
        ("   ", "ncbi_mesh", None),                       # whitespace only
        ("not an id", "ncbi_mesh", None),                 # unrecognised shape
    ]

    for value, database, expected in cases:
        assert normalise_identifier(value, database) == expected, (
            f"normalise_identifier({value!r}, {database!r}) should be {expected!r}"
        )


def test_author_display_contract() -> None:
    """Both name halves, each half alone, and neither — in one context."""
    cases: list[tuple[Optional[str], Optional[str], str]] = [
        ("German", "Alexander J.", "German Alexander J."),
        ("German", None, "German"),
        (None, "Alexander", "Alexander"),
        (None, None, ""),
    ]

    for surname, given, expected in cases:
        author = Author(surname=surname, given_names=given)
        assert author.display == expected, f"{surname!r}/{given!r} -> {expected!r}"


def test_paper_importable_contract() -> None:
    """Both halves of the `and`, in both orders, in one context."""
    cases: list[tuple[Optional[str], bool, bool]] = [
        ("PMC1234567", True, True),
        ("PMC1234567", False, False),
        (None, True, False),
        (None, False, False),
        ("", True, False),
    ]

    for pmcid, has_full_text, expected in cases:
        paper = PaperResponse(pmcid=pmcid, has_full_text=has_full_text)
        assert paper.importable is expected, f"{pmcid!r}/{has_full_text} -> {expected}"
