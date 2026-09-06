"""Build the graph from Postgres: apply the schema, then load the corpus.

    python -m api.graph.build              # schema + load
    python -m api.graph.build --schema-only
    python -m api.graph.build --reset      # wipe the graph first

Idempotent, so re-running is the normal way to refresh after more papers are
imported. `--reset` deletes every node and relationship but leaves constraints
and indexes in place — the schema is not the thing being rebuilt.
"""

from __future__ import annotations

import argparse
import logging
import sys

from api.db.base import session_scope
from api.graph import export, schema
from api.graph.driver import driver_scope, run, run_implicit, verify_connectivity

log = logging.getLogger("api.graph.build")


def reset() -> int:
    """Delete every node and relationship. Returns the node count removed.

    Batched, so a large corpus does not have to fit in one transaction — which
    is also why it needs an implicit transaction rather than `run`.
    """
    before = run("MATCH (n) RETURN count(n) AS nodes")[0]["nodes"]
    run_implicit("MATCH (n) CALL (n) { DETACH DELETE n } IN TRANSACTIONS OF 10000 ROWS")
    return before


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--schema-only", action="store_true", help="apply constraints, load nothing"
    )
    parser.add_argument(
        "--reset", action="store_true", help="delete all nodes and edges first"
    )
    args = parser.parse_args(argv)

    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")
    # Every IF NOT EXISTS no-op raises an INFO notification. That is the point
    # of a re-run, not news, and it drowns the counts we actually want to read.
    logging.getLogger("neo4j.notifications").setLevel(logging.WARNING)

    with driver_scope():
        verify_connectivity()

        if args.reset:
            log.info("reset: removed %d node(s)", reset())

        log.info("applied %d schema statement(s)", schema.apply())

        if not args.schema_only:
            with session_scope() as session:
                counts = export.load_all(session)
            for step, count in counts._asdict().items():
                log.info("  %-16s %6d", step, count)

        state = schema.state()
        log.info("constraints: %s", ", ".join(state.constraints))
        log.info("labels:      %s", ", ".join(state.labels) or "(none)")
        log.info("rel types:   %s", ", ".join(state.relationship_types) or "(none)")

    return 0


if __name__ == "__main__":
    sys.exit(main())
