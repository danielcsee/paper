"""The Neo4j connection: one lazily-built driver, shared by every caller.

Mirrors `api.db.base`: build on first use, hand out a context manager, and keep
the connection details in `Settings` rather than at each call site.
"""

from __future__ import annotations

from contextlib import contextmanager
from typing import Any, Iterator, Mapping, Optional, Sequence

from neo4j import Driver, GraphDatabase

from api.app.config import get_settings

_driver: Optional[Driver] = None


def get_driver() -> Driver:
    """The process-wide driver. Cheap to call; the driver pools connections."""
    global _driver
    if _driver is None:
        settings = get_settings()
        _driver = GraphDatabase.driver(
            settings.neo4j_uri, auth=settings.neo4j_credentials
        )
    return _driver


def close_driver() -> None:
    """Release the pool. For process shutdown and tests."""
    global _driver
    if _driver is not None:
        _driver.close()
        _driver = None


@contextmanager
def driver_scope() -> Iterator[Driver]:
    """A driver that closes on the way out — for scripts, not for the server."""
    try:
        yield get_driver()
    finally:
        close_driver()


def run(query: str, /, **parameters: Any) -> list[Any]:
    """Run one query against the configured database and return its records."""
    result = get_driver().execute_query(
        query, parameters_=parameters, database_=get_settings().neo4j_database
    )
    return list(result.records)


def run_batched(
    query: str,
    rows: Sequence[Mapping[str, Any]],
    *,
    batch_size: int = 5_000,
) -> int:
    """Feed `rows` through `query` as `$rows`, in batches. Returns the row count.

    Every write in this package is an `UNWIND $rows`, so one round trip carries
    thousands of nodes instead of one. Batching also keeps a single transaction
    from holding the whole corpus in memory.
    """
    for start in range(0, len(rows), batch_size):
        run(query, rows=list(rows[start : start + batch_size]))
    return len(rows)


def run_implicit(query: str, /, **parameters: Any) -> list[Any]:
    """Run one query in an *implicit* (auto-commit) transaction.

    `CALL { ... } IN TRANSACTIONS` manages its own transactions and is refused
    inside the explicit one that `execute_query` opens. Only batched writes of
    that shape need this; everything else should use `run`.
    """
    with get_driver().session(database=get_settings().neo4j_database) as session:
        return list(session.run(query, **parameters))


def verify_connectivity() -> None:
    """Raise if Neo4j is unreachable, with the driver's own diagnostics."""
    get_driver().verify_connectivity()
