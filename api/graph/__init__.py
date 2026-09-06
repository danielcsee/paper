"""The Neo4j knowledge graph: schema, identity keys, and the Postgres export.

Public interface:

    from api.graph import load_all, get_driver
    from api.graph import schema

The whole job — apply the schema, then project the corpus — is the CLI in
`build.py`, run as `python -m api.graph.build`. It is deliberately not imported
here: importing a `__main__` module from its own package makes runpy load it
twice.
"""

from api.graph.driver import close_driver, driver_scope, get_driver, run, run_batched
from api.graph.export import LoadCounts, load_all
from api.graph.keys import author_key, entity_key, entity_labels
from api.graph.schema import SchemaState

__all__ = [
    "LoadCounts",
    "SchemaState",
    "author_key",
    "close_driver",
    "driver_scope",
    "entity_key",
    "entity_labels",
    "get_driver",
    "load_all",
    "run",
    "run_batched",
]
