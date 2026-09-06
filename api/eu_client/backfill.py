"""Name the entities already stored without one.

    python -m api.eu_client.backfill          # resolve and write
    python -m api.eu_client.backfill --dry-run

A one-off for rows imported before names were resolved; new papers are handled
by the ingest task. Safe to re-run — it only ever looks at rows whose name is
still their own identifier.
"""

from __future__ import annotations

import argparse
import asyncio
import logging

from api.db import session_scope
from api.eu_client.naming import apply_names, resolve_names, unnamed_entities

log = logging.getLogger(__name__)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="resolve, print, write nothing")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(message)s")

    with session_scope() as session:
        pending = unnamed_entities(session)
        if not pending:
            print("nothing to do: every resolvable entity already has a name")
            return 0
        print(f"{len(pending)} entit{'y' if len(pending) == 1 else 'ies'} without a name")

        names = asyncio.run(resolve_names(pending))
        by_id = {entity.id: entity for entity in pending}
        for entity_id, name in sorted(names.items()):
            entity = by_id[entity_id]
            print(f"  {entity.identifier:<24} {entity.name!r} -> {name!r}")

        missing = [entity for entity in pending if entity.id not in names]
        for entity in missing:
            print(f"  {entity.identifier:<24} unresolved, keeping {entity.name!r}")

        if args.dry_run:
            print(f"\ndry run: {len(names)} name(s) would be written")
            session.rollback()
            return 0

        written = apply_names(session, names)
        print(f"\nwrote {written} name(s); {len(missing)} left as they were")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
