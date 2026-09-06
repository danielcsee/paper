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
from api.db.models import Entity, PaperEntityMention
from api.eu_client.eutils import RESOLVABLE, EutilsClient
from api.ncbi import http as ncbi_http

log = logging.getLogger(__name__)


def unnamed_entities(session: Session, paper_id: Optional[int] = None) -> list[Entity]:
    """Entities whose `name` is just the identifier's local part.

    That is exactly the shape PubTator produces when it has no label — Species
    come through named "9606" — so it is also the test for "needs a name".
    Restricted to databases E-utilities can actually resolve.

    `paper_id` narrows it to concepts that paper mentions, which is what the
    ingest task wants: unscoped, every import would re-ask about the same
    permanently unresolvable ids. Three taxa in this corpus are `status:
    merged` at NCBI and return empty names forever.
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
    if paper_id is not None:
        statement = statement.where(
            Entity.id.in_(
                select(PaperEntityMention.entity_id).where(
                    PaperEntityMention.paper_id == paper_id
                )
            )
        )
    return list(session.execute(statement).scalars().all())


async def resolve_names(entities: Sequence[Entity]) -> dict[int, str]:
    """entity id -> name, for those NCBI could name. One request per database.

    Builds its own HTTP client: this runs from a Celery task and from a
    command-line backfill, neither of which has one to hand. The rate limiter
    is the shared Redis one, so these calls draw on the same NCBI budget as
    everything else.
    """
    by_database: dict[str, dict[str, int]] = {}
    for entity in entities:
        eutils_db = RESOLVABLE.get(entity.database)
        if eutils_db is None:
            continue
        uid = entity.identifier.split(":", 1)[-1]
        by_database.setdefault(eutils_db, {})[uid] = entity.id
    if not by_database:
        return {}

    settings = get_settings()
    resolved: dict[int, str] = {}
    async with ncbi_http.build_client(
        timeout=settings.http_timeout_seconds,
        contact_email=settings.ncbi_contact_email,
        limiter=ncbi_http.RedisRateLimiter(
            settings.rate_limit_redis_url, settings.ncbi_rate_limit_per_second
        ),
    ) as client:
        eutils = EutilsClient(client, settings.eutils_base_url)
        for eutils_db, uids in by_database.items():
            names = await eutils.names(eutils_db, list(uids))
            for uid, concept in names.items():
                entity_id = uids.get(uid)
                if entity_id is not None:
                    resolved[entity_id] = concept.name
    return resolved


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
