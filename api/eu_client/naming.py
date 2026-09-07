"""Put real names on entities whose `name` is only their identifier again.

One implementation, two callers: the backfill for rows already stored, and the
ingest task for rows a new paper adds. Both are best-effort — a concept that
cannot be named keeps the label it has, and nothing upstream of this fails.
"""

from __future__ import annotations

import logging
from typing import Optional, Sequence

from sqlalchemy import func, or_, select, update
from sqlalchemy.orm import Session

from api.app.config import get_settings
from api.db.models import Entity
from api.eu_client.eutils import RESOLVABLE, EutilsClient
from api.ncbi import http as ncbi_http

log = logging.getLogger(__name__)


def already_named(session: Session, identifiers: Sequence[str]) -> set[str]:
    """Which of these identifiers already carry a real name.

    The step that keeps this cheap: of 116 concepts across the stored corpus,
    113 were already named, so only 3 ever reached E-utilities.
    """
    if not identifiers:
        return set()
    rows = session.execute(
        select(Entity.identifier).where(
            Entity.identifier.in_(tuple(identifiers)),
            Entity.name.is_not(None),
            Entity.name != func.split_part(Entity.identifier, ":", 2),
        )
    ).scalars().all()
    return set(rows)


async def resolve_identifiers(concepts: dict[str, str]) -> dict[str, str]:
    """identifier -> name, for the ids NCBI can name. One request per database.

    Builds its own HTTP client and holds no transaction: this runs between the
    fetch and the write, and a rate-limited network call inside a transaction is
    how short locks become long ones.
    """
    by_database: dict[str, dict[str, str]] = {}
    for identifier, database in concepts.items():
        eutils_db = RESOLVABLE.get(database)
        if eutils_db is None:
            continue
        by_database.setdefault(eutils_db, {})[identifier.split(":", 1)[-1]] = identifier
    if not by_database:
        return {}

    settings = get_settings()
    resolved: dict[str, str] = {}
    async with ncbi_http.build_client(
        timeout=settings.http_timeout_seconds,
        contact_email=settings.ncbi_contact_email,
        limiter=ncbi_http.RedisRateLimiter(
            settings.rate_limit_redis_url, settings.ncbi_rate_limit_per_second
        ),
    ) as client:
        eutils = EutilsClient(client, settings.eutils_base_url)
        for eutils_db, uids in by_database.items():
            for uid, concept in (await eutils.names(eutils_db, list(uids))).items():
                identifier = uids.get(uid)
                if identifier is not None:
                    resolved[identifier] = concept.name
    return resolved


def unnamed_entities(session: Session) -> list[Entity]:
    """Entities whose `name` is just the identifier's local part.

    That is exactly the shape PubTator produces when it has no label — Species
    come through named "9606" — so it is also the test for "needs a name".
    Restricted to databases E-utilities can actually resolve. Used by the
    backfill, which repairs whatever is already stored; ingest resolves from the
    paper in memory instead, before anything is written.
    """
    # Built from constructs, not a text() fragment: a raw "A OR B" splices in
    # unparenthesised and binds looser than the AND beside it, which quietly
    # matched every unnamed row regardless of its database.
    statement = select(Entity).where(
        Entity.database.in_(tuple(RESOLVABLE)),
        or_(
            Entity.name.is_(None),
            Entity.name == func.split_part(Entity.identifier, ":", 2),
        ),
    )
    return list(session.execute(statement).scalars().all())


async def resolve_names(entities: Sequence[Entity]) -> dict[int, str]:
    """entity id -> name, for stored rows. The backfill's view of the same job."""
    by_identifier = {entity.identifier: entity.database for entity in entities}
    names = await resolve_identifiers(by_identifier)
    return {
        entity.id: names[entity.identifier]
        for entity in entities
        if entity.identifier in names
    }


def apply_names(session: Session, names: dict[int, str]) -> int:
    """Write the resolved names. Returns how many rows changed."""
    if not names:
        return 0
    # SQLAlchemy 2.0's bulk UPDATE by primary key: one statement, the rows
    # carry their own `id`. Writing the WHERE by hand instead makes the ORM
    # treat it as a criteria update and refuse to reconcile loaded objects.
    # Sorted by primary key so concurrent backfills lock rows in one order.
    # An UPDATE here contends with the ON CONFLICT DO UPDATE in
    # `persist.upsert_entities`, which sorts for the same reason.
    session.execute(
        update(Entity),
        [{"id": entity_id, "name": names[entity_id]} for entity_id in sorted(names)],
    )
    return len(names)
