"""Shared HTTP plumbing: one connection pool, NCBI-polite headers, light retries."""

from __future__ import annotations

import asyncio
import logging

import httpx

from api.pb_client.errors import UpstreamError

log = logging.getLogger(__name__)

#: NCBI asks that clients identify themselves and stay under ~3 requests/second.
USER_AGENT = "litgraph/0.1 (https://github.com/; research prototype)"

RETRY_STATUSES = frozenset({429, 500, 502, 503, 504})


def build_client(timeout: float, contact_email: str | None = None) -> httpx.AsyncClient:
    """One AsyncClient for the process, so connections are pooled and reused."""
    user_agent = USER_AGENT
    if contact_email:
        user_agent = f"{USER_AGENT[:-1]}; {contact_email})"
    return httpx.AsyncClient(
        timeout=httpx.Timeout(timeout),
        headers={"User-Agent": user_agent, "Accept-Encoding": "gzip"},
        follow_redirects=True,
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
