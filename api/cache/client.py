"""One async Redis connection per event loop.

Per loop, not per process. The Celery worker runs each task inside its own
`asyncio.run`, so a module-level client would be bound to a loop that has
already closed by the time the next task starts. The map is keyed on the
running loop and holds it weakly, so entries disappear with the loop itself.
"""

from __future__ import annotations

import asyncio
import logging
import weakref
from typing import Optional

import redis.asyncio as aioredis

log = logging.getLogger(__name__)

#: loop -> {url: client}. Keyed on the URL as well as the loop: two caches
#: pointing at different instances must not be handed each other's connection.
_clients: "weakref.WeakKeyDictionary[asyncio.AbstractEventLoop, dict]" = (
    weakref.WeakKeyDictionary()
)


def get_client(url: str, timeout: float) -> Optional[aioredis.Redis]:
    """The client for the running loop, created on first use.

    Returns None if there is no running loop or the client cannot be built.
    Callers treat None as "cache unavailable" and carry on.
    """
    try:
        loop = asyncio.get_running_loop()
    except RuntimeError:
        return None

    per_url = _clients.setdefault(loop, {})
    client = per_url.get(url)
    if client is None:
        try:
            client = aioredis.from_url(
                url,
                socket_timeout=timeout,
                socket_connect_timeout=timeout,
                # Documents are bytes; decoding them as text would only mean
                # re-encoding to parse the JSON.
                decode_responses=False,
            )
        except Exception:  # malformed URL, unsupported scheme
            log.warning("could not build a Redis cache client for %s", url, exc_info=True)
            return None
        per_url[url] = client
    return client


async def close_client(url: str) -> None:
    """Close this loop's client for `url`, if it has one.

    The worker runs every task in its own `asyncio.run`, so without this each
    task would leave a connection pool behind for the garbage collector to drop
    unclosed. The web process has one long-lived loop and closes at shutdown.
    """
    try:
        loop = asyncio.get_running_loop()
    except RuntimeError:
        return
    client = (_clients.get(loop) or {}).pop(url, None)
    if client is None:
        return
    try:
        await client.aclose()
    except Exception:  # already closed, or never connected
        log.debug("closing the Redis cache client for %s failed", url, exc_info=True)
