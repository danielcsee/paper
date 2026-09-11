"""`api.ingestion.persist` — the pure half of the write path.

Everything here runs on an in-memory `PaperResponse`, before anything touches
Postgres. These are the rules that decide *whether a concept is admitted at
all* and under *which* identifier, so a regression here is silent: the import
still succeeds, and the graph quietly grows a duplicate or loses a row.

The functions that take a `Session` are not covered here. They need a live
database and belong in an integration suite.
"""

from __future__ import annotations

import datetime as dt
from typing import Optional

import pytest

from api.ingestion.persist import (
    FINAL_STAGE,
    PREFIX_DATABASES,
    TYPE_DATABASES,
    _parse_date,
    _parse_year,
    concept_databases,
    derive_state,
    source_database,
    unnamed_concepts,
)
from api.pb_client.models import Annotation, PaperResponse, Passage, Relation


def _annotation(
    identifier: Optional[str],
    *,
    name: Optional[str] = None,
    type_: Optional[str] = None,
    database: Optional[str] = None,
    grounded: bool = True,
) -> Annotation:
    """An annotation with only the fields these rules read."""
    return Annotation(
        offset=0,
        length=1,
        identifier=identifier,
        name=name,
        type=type_,
        database=database,
        grounded=grounded,
    )


def _paper(
    annotations: Optional[list[Annotation]] = None,
    relations: Optional[list[Relation]] = None,
) -> PaperResponse:
    """A paper carrying one passage, or none when there is nothing to hold."""
    passages = (
        [Passage(ordinal=0, offset=0, text="body", annotations=annotations)]
        if annotations
        else []
    )
    return PaperResponse(pmid=1, passages=passages, relations=relations or [])


# ------------------------------------------------------------------ derive_state


def test_derive_state_reports_failure_before_success() -> None:
    """Order is the contract: a failed later stage is an error, not a success.

    `stages` arrives unordered from the ledger, so the precedence has to be in
    the code rather than in the iteration.
    """
    cases: list[tuple[dict[str, str], str]] = [
        ({}, "queued"),
        ({"ingest": "pending"}, "queued"),
        ({"ingest": "pending", "embed": "pending"}, "queued"),
        ({"ingest": "running"}, "started"),
        ({"ingest": "done"}, "started"),
        ({"ingest": "done", "embed": "done"}, "started"),
        ({"ingest": "done", "embed": "done", FINAL_STAGE: "done"}, "success"),
        ({"ingest": "failed"}, "error"),
        # Failure wins even when every other stage, including the last, is done.
        ({"ingest": "done", "embed": "failed", FINAL_STAGE: "done"}, "error"),
        ({FINAL_STAGE: "failed"}, "error"),
    ]

    for stages, expected in cases:
        assert derive_state(stages) == expected, stages


def test_derive_state_requires_the_final_stage_for_success() -> None:
    """A paper is not 'imported' until it is in the graph."""
    assert derive_state({"ingest": "done", "embed": "done"}) == "started"
    assert derive_state({FINAL_STAGE: "done"}) == "success"


# ------------------------------------------------------------ concept_databases


def test_concept_databases_indexes_identifier_and_bare_suffix() -> None:
    """A relation role carries no database, so both spellings must resolve.

    The role names the concept as upstream wrote it, which may be the bare
    suffix of an id an annotation reported in qualified form.
    """
    paper = _paper([_annotation("ncbi_gene:672", database="ncbi_gene")])

    databases = concept_databases(paper)

    assert databases["ncbi_gene:672"] == "ncbi_gene"
    assert databases["672"] == "ncbi_gene"
    # And by namespace, for a sibling id no annotation mentioned.
    assert databases["ncbi_gene"] == "ncbi_gene"


def test_concept_databases_ignores_entries_without_provenance() -> None:
    """No database means no claim: these must not enter the map at all."""
    paper = _paper(
        [
            _annotation("111", database=None),
            _annotation("", database="ncbi_gene"),
            _annotation("   ", database="ncbi_gene"),
            _annotation(None, database="ncbi_gene"),
        ]
    )

    assert concept_databases(paper) == {}


def test_concept_databases_strips_whitespace_and_keeps_the_first_namespace() -> None:
    """`setdefault` on the namespace: the first annotation to claim it wins."""
    paper = _paper(
        [
            _annotation("  MESH:D001943  ", database="ncbi_mesh"),
            _annotation("MESH:D002289", database="other_source"),
        ]
    )

    databases = concept_databases(paper)

    assert databases["MESH:D001943"] == "ncbi_mesh"
    assert databases["D001943"] == "ncbi_mesh"
    assert databases["MESH"] == "ncbi_mesh"
    # The second annotation still registers under its own keys.
    assert databases["MESH:D002289"] == "other_source"


# ------------------------------------------------------------- source_database


def test_source_database_resolution_order() -> None:
    """Precedence: explicit, sibling id, sibling namespace, type, then prefix."""
    siblings = {"672": "ncbi_gene", "MESH": "ncbi_mesh"}
    cases: list[tuple[str, Optional[str], Optional[str], Optional[str]]] = [
        # identifier, kind, explicit, expected
        ("672", "Gene", "from_upstream", "from_upstream"),
        ("672", None, None, "ncbi_gene"),
        ("MESH:D999", None, None, "ncbi_mesh"),
        ("9606", "Species", None, "ncbi_taxonomy"),
        ("9606", "Gene", None, "ncbi_gene"),
        ("CVCL:0031", None, None, "cvcl"),
        ("cvcl:0031", None, None, "cvcl"),
        ("OMIM:114480", None, None, "omim"),
        ("UNKNOWN:1", None, None, None),
        ("12345", None, None, None),
        ("12345", "Disease", None, None),
    ]

    for identifier, kind, explicit, expected in cases:
        assert source_database(identifier, kind, siblings, explicit) == expected, identifier


def test_source_database_prefers_explicit_over_everything() -> None:
    """What upstream said outright is never overridden by inference."""
    siblings = {"672": "ncbi_gene"}

    assert source_database("672", "Gene", siblings, "  trimmed  ") == "trimmed"


def test_source_database_treats_every_spelling_of_absent_alike() -> None:
    """None, "" and "   " all mean upstream stated nothing: fall through.

    `explicit` is `Annotation.database` off PubTator's `infons`, where a blank
    is indistinguishable from a missing key. Rejecting on a blank would drop a
    concept the sibling annotation, the type or the prefix could still ground,
    and would do it only for one of the three spellings.
    """
    siblings = {"672": "ncbi_gene"}
    blanks: list[Optional[str]] = [None, "", " ", "\t", "   \n "]

    for explicit in blanks:
        assert source_database("672", "Gene", siblings, explicit) == "ncbi_gene", repr(explicit)


def test_source_database_falls_through_a_blank_rather_than_rejecting() -> None:
    """The fall-through must reach every later step, not just the sibling map.

    A blank used to return None outright, so nothing below it ran. These are
    the cases that had no sibling entry to save them.
    """
    cases: list[tuple[str, Optional[str], Optional[str]]] = [
        # identifier, kind, expected -- with no sibling annotations at all
        ("9606", "Species", "ncbi_taxonomy"),
        ("672", "Gene", "ncbi_gene"),
        ("MESH:D001943", None, "ncbi_mesh"),
        ("UNKNOWN:1", None, None),
    ]

    for identifier, kind, expected in cases:
        assert source_database(identifier, kind, {}, " ") == expected, identifier


def test_source_database_tables_cover_the_bare_id_types() -> None:
    """Gene and Species are the only kinds whose ids arrive unqualified."""
    assert TYPE_DATABASES == {"Gene": "ncbi_gene", "Species": "ncbi_taxonomy"}
    assert set(PREFIX_DATABASES) == {"MESH", "CVCL", "OMIM"}


# ------------------------------------------------------------ unnamed_concepts


def test_unnamed_concepts_finds_relation_only_concepts() -> None:
    """A concept named only by a relation has no mention row to find it by.

    PubTator has no label for Species or CellLine and sends the identifier in
    the name field, which is the signature this looks for.
    """
    paper = _paper(
        annotations=[_annotation("9606", name="9606", type_="Species", database="ncbi_taxonomy")],
        relations=[
            Relation(
                role1_identifier="9685",
                role1_name="9685",
                role1_type="Species",
                role2_identifier="672",
                role2_name="BRCA1",
                role2_type="Gene",
            )
        ],
    )

    concepts = unnamed_concepts(paper)

    assert concepts["ncbi_taxonomy:9606"] == "ncbi_taxonomy"
    # Named only by the relation, and qualified from its type.
    assert concepts["ncbi_taxonomy:9685"] == "ncbi_taxonomy"
    # Already carries a real name, so it needs no resolution.
    assert "ncbi_gene:672" not in concepts


def test_unnamed_concepts_treats_a_real_name_as_named() -> None:
    """Only an empty name, or the bare local id, counts as unnamed."""
    paper = _paper(
        [
            _annotation("672", name="BRCA1", type_="Gene", database="ncbi_gene"),
            _annotation("673", name="", type_="Gene", database="ncbi_gene"),
            _annotation("674", name="  ", type_="Gene", database="ncbi_gene"),
            _annotation("675", name="675", type_="Gene", database="ncbi_gene"),
            _annotation("676", name="ncbi_gene:676", type_="Gene", database="ncbi_gene"),
        ]
    )

    concepts = unnamed_concepts(paper)

    assert "ncbi_gene:672" not in concepts
    assert "ncbi_gene:673" in concepts
    assert "ncbi_gene:674" in concepts
    assert "ncbi_gene:675" in concepts
    # The qualified form is not the local id, so this reads as named.
    assert "ncbi_gene:676" not in concepts


def test_unnamed_concepts_drops_what_it_cannot_ground() -> None:
    """Ungrounded, unparseable, or unprovenanced concepts are never invented."""
    paper = _paper(
        annotations=[
            _annotation("9606", name="9606", type_="Species", grounded=False),
            _annotation("-", name="-", type_="Species", database="ncbi_taxonomy"),
            _annotation("12345", name="12345", type_="Disease"),
        ]
    )

    assert unnamed_concepts(paper) == {}


def test_unnamed_concepts_handles_a_paper_with_nothing_to_resolve() -> None:
    """The common case on an abstract-only response."""
    assert unnamed_concepts(_paper()) == {}


# -------------------------------------------------------------- date parsing


def test_parse_date_and_year_tolerate_pubtator_shapes() -> None:
    """Upstream dates are frequently absent or malformed; None, never a raise."""
    date_cases: list[tuple[Optional[str], Optional[dt.date]]] = [
        ("2002-04-22T00:00:00Z", dt.date(2002, 4, 22)),
        ("2002-04-22", dt.date(2002, 4, 22)),
        ("2002-04-22T00:00:00+00:00", dt.date(2002, 4, 22)),
        (None, None),
        ("", None),
        ("not a date", None),
        ("2002-13-01", None),
    ]

    for value, expected in date_cases:
        assert _parse_date(value) == expected, value


def test_parse_year_prefers_the_passage_year() -> None:
    """`year` is absent on abstract-only responses; the date is the fallback."""
    year_cases: list[tuple[Optional[str], Optional[str], Optional[int]]] = [
        ("2002", "1999-01-01T00:00:00Z", 2002),
        (None, "1999-01-01T00:00:00Z", 1999),
        ("", "1999-01-01T00:00:00Z", 1999),
        ("n/a", "1999-01-01T00:00:00Z", 1999),
        ("2002", None, 2002),
        (None, None, None),
        (None, "not-a-date", None),
        ("n/a", "n/a", None),
    ]

    for year, date, expected in year_cases:
        assert _parse_year(year, date) == expected, (year, date)


@pytest.mark.parametrize("value", ["2002-04-22T00:00:00Z", "2002-04-22"])
def test_parse_date_accepts_both_pubtator_spellings(value: str) -> None:
    assert _parse_date(value) == dt.date(2002, 4, 22)
