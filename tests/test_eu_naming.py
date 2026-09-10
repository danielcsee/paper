"""`api.eu_client.naming` — the paths that need neither Postgres nor NCBI.

The rest of this module is bound to a live `Session` (its predicates use the
Postgres-only `split_part`) or to E-utilities over HTTP, so it belongs in an
integration suite. Those symbols are recorded in Testledger as explicit skip
dispositions rather than left as silent coverage gaps.
"""

from __future__ import annotations

import asyncio

from api.db.models import Entity
from api.eu_client.eutils import RESOLVABLE
from api.eu_client.naming import already_named, apply_names, resolve_identifiers


def test_already_named_short_circuits_on_empty_input() -> None:
    """The empty case returns before touching the session at all.

    Passing None proves no query is attempted: a session would be dereferenced.
    """
    assert already_named(None, []) == set()  # type: ignore[arg-type]
    assert already_named(None, ()) == set()  # type: ignore[arg-type]


def test_resolve_identifiers_returns_empty_without_a_resolvable_database() -> None:
    """No resolvable database means no HTTP client is ever built.

    E-utilities resolves taxonomy and gene only; MeSH and Cellosaurus ids are
    named upstream and must not trigger a request.
    """
    concepts = {"MESH:D002118": "ncbi_mesh", "CVCL:0031": "cvcl"}

    assert asyncio.run(resolve_identifiers(concepts)) == {}
    assert asyncio.run(resolve_identifiers({})) == {}


def test_resolvable_databases_are_the_documented_pair() -> None:
    """`unnamed_entities` and `resolve_identifiers` both gate on this map."""
    assert RESOLVABLE == {"ncbi_taxonomy": "taxonomy", "ncbi_gene": "gene"}


def test_apply_names_writes_nothing_for_an_empty_mapping() -> None:
    """Best-effort: nothing to resolve must not open a write transaction."""
    assert apply_names(None, {}) == 0  # type: ignore[arg-type]


def test_entity_rows_can_be_built_without_a_session() -> None:
    """Guards the fixture shape the integration suite will reuse."""
    entity = Entity(
        id=1, identifier="ncbi_gene:672", entity_type="Gene", database="ncbi_gene"
    )

    assert entity.identifier == "ncbi_gene:672"
    assert entity.database in RESOLVABLE
