"""Shared HTTP plumbing: one connection pool, NCBI-polite headers, light retries.

The rate limit lives in a transport rather than in `get()` below, because `get`
is not the only way out of the client: `pm_client` streams file downloads
straight off the AsyncClient, and `follow_redirects=True` means one call can become
several requests. A transport sees all of them.
"""

from __future__ import annotations

import asyncio
import logging
import threading
import time
from typing import Optional

import httpx

from api.ncbi.errors import UpstreamError
from api.redis_conn import get_client

log = logging.getLogger(__name__)

#: NCBI asks that clients identify themselves and stay under ~3 requests/second.
USER_AGENT = "litgraph/0.1 (https://github.com/; research prototype)"

#: Requests per second allowed against NCBI. A property of their service, not of
#: our deployment, so it is a constant here rather than a setting.
NCBI_REQUESTS_PER_SECOND = 3.0

RETRY_STATUSES = frozenset({429, 500, 502, 503, 504})


class RateLimiter:
    """Hands out evenly spaced request slots, at most `rate` per second.

    Strict spacing rather than a token bucket: a bucket would let three requests
    leave in the same millisecond, which is compliant only if the server counts
    in aligned one-second windows. Spacing them 1/rate apart is under the limit
    under any accounting.

    Deliberately not an `asyncio.Lock`. On Python 3.9 asyncio primitives bind to
    the loop that created them, and the Celery worker builds a fresh client
    inside a fresh `asyncio.run` for every paper — a loop-bound lock could not be
    shared across those. This lock is held for a few microseconds of arithmetic
    and never across an `await`, so it never blocks the event loop.
    """

    def __init__(self, rate: float) -> None:
        if rate <= 0:
            raise ValueError("rate must be positive")
        self._interval = 1.0 / rate
        self._lock = threading.Lock()
        self._next_slot = 0.0

    async def acquire(self) -> None:
        """Wait until this caller's slot comes up. Claims it first, then sleeps."""
        now = time.monotonic()
        with self._lock:
            slot = max(now, self._next_slot)
            self._next_slot = slot + self._interval
        delay = slot - now
        if delay > 0:
            await asyncio.sleep(delay)


#: The in-process fallback, used when Redis cannot be reached. Module-level on
#: purpose: the worker builds one client per paper, so a per-client limiter
#: would reset on every task and enforce nothing across them.
_shared_limiter = RateLimiter(NCBI_REQUESTS_PER_SECOND)


#: Claim the next free slot, atomically, and report how long to wait for it.
#:
#: The same algorithm as `RateLimiter` above, moved into the server so every
#: process draws on one budget. `TIME` is Redis's own clock, not the caller's:
#: the worker and the web process do not share one, and in containers they
#: drift. Times cross the Lua boundary as integer microseconds, since Redis
#: truncates a float return.
_CLAIM_SLOT = """
local interval = tonumber(ARGV[1])
local ttl_ms   = tonumber(ARGV[2])
local t        = redis.call('TIME')
local now      = tonumber(t[1]) * 1000000 + tonumber(t[2])
local slot     = tonumber(redis.call('GET', KEYS[1]) or '0')
if slot < now then slot = now end
redis.call('SET', KEYS[1], slot + interval, 'PX', ttl_ms)
return slot - now
"""


class RedisRateLimiter:
    """One request budget shared by every process talking to NCBI.

    Same contract as `RateLimiter` — `await acquire()` returns when the caller
    may proceed — so `RateLimitedTransport` cannot tell them apart.

    Unlike the local limiter this object holds no state worth preserving: the
    slot lives in Redis, so it no longer matters that the worker builds a fresh
    limiter for every task.

    If Redis cannot be reached, it falls back to the process-wide local
    limiter. That degrades the guarantee to 3/s *per process* rather than
    dropping it, which is the safe direction — and in the worker's case Redis
    being down means the broker is down too, so nothing is running anyway.
    """

    def __init__(
        self,
        url: str,
        rate: float = NCBI_REQUESTS_PER_SECOND,
        *,
        key: str = "ncbi:ratelimit",
        timeout: float = 1.0,
        fallback: Optional[RateLimiter] = None,
    ) -> None:
        if rate <= 0:
            raise ValueError("rate must be positive")
        self._url = url
        self._key = key
        self._timeout = timeout
        self._interval_us = int(1_000_000 / rate)
        # Long enough that an idle gap never expires a live claim; short enough
        # that a forgotten key does not outlive the process. Expiry is harmless
        # either way — it just restarts the sequence.
        self._ttl_ms = max(10_000, self._interval_us // 1000 * 10)
        self._fallback = fallback if fallback is not None else _shared_limiter
        self._script = None

    async def acquire(self) -> None:
        delay = await self._claim()
        if delay is None:
            await self._fallback.acquire()
            return
        if delay > 0:
            await asyncio.sleep(delay)

    async def _claim(self) -> Optional[float]:
        """Seconds to wait, or None if Redis could not answer."""
        client = get_client(self._url, self._timeout)
        if client is None:
            return None
        try:
            # register_script gives EVALSHA with an EVAL fallback, so a Redis
            # restart that flushed the script cache recovers by itself.
            if self._script is None:
                self._script = client.register_script(_CLAIM_SLOT)
            micros = await self._script(
                keys=[self._key], args=[self._interval_us, self._ttl_ms]
            )
        except Exception as exc:
            log.warning(
                "NCBI rate limiter could not reach Redis (%s); "
                "falling back to this process's own limit",
                exc,
            )
            return None
        return max(0.0, int(micros) / 1_000_000)


class RateLimitedTransport(httpx.AsyncBaseTransport):
    """Waits for a slot, then delegates. Wraps the real transport, owns closing it."""

    def __init__(
        self, limiter: RateLimiter, inner: Optional[httpx.AsyncBaseTransport] = None
    ) -> None:
        self._limiter = limiter
        self._inner = inner if inner is not None else httpx.AsyncHTTPTransport()

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        await self._limiter.acquire()
        return await self._inner.handle_async_request(request)

    async def aclose(self) -> None:
        # AsyncBaseTransport.aclose is a no-op and AsyncClient closes only the
        # transport it was handed, so without this the pool would leak.
        await self._inner.aclose()


def build_client(
    timeout: float,
    contact_email: str | None = None,
    limiter: RateLimiter | None = None,
) -> httpx.AsyncClient:
    """One AsyncClient for the process, so connections are pooled and reused.

    Every request it makes is rate limited. httpx has no parameter for this —
    `limits=` caps concurrent connections, which is not a rate — so the limit is
    installed as a custom `transport=`.
    """
    user_agent = USER_AGENT
    if contact_email:
        user_agent = f"{USER_AGENT[:-1]}; {contact_email})"
    return httpx.AsyncClient(
        timeout=httpx.Timeout(timeout),
        headers={"User-Agent": user_agent, "Accept-Encoding": "gzip"},
        follow_redirects=True,
        transport=RateLimitedTransport(limiter if limiter is not None else _shared_limiter),
    )


async def get(
    client: httpx.AsyncClient,
    url: str,
    *,
    params: dict | None = None,
    attempts: int = 3,
    backoff: float = 1.0,
) -> httpx.Response:
    """GET with retries on transport errors and transient upstream statuses.

    4xx other than 429 is returned to the caller rather than retried — those are
    our fault (bad PMCID, bad parameter) and will fail identically next time.
    """
    last_error: Exception | None = None
    for attempt in range(1, attempts + 1):
        try:
            response = await client.get(url, params=params)
        except httpx.HTTPError as exc:  # timeouts, DNS, connection resets
            last_error = exc
            log.warning("GET %s failed (attempt %d/%d): %s", url, attempt, attempts, exc)
        else:
            if response.status_code not in RETRY_STATUSES:
                return response
            last_error = UpstreamError(f"HTTP {response.status_code} from {url}")
            log.warning(
                "GET %s -> %d (attempt %d/%d)", url, response.status_code, attempt, attempts
            )
        if attempt < attempts:
            await asyncio.sleep(backoff * 2 ** (attempt - 1))

    raise UpstreamError(f"{url} unavailable after {attempts} attempts: {last_error}")
