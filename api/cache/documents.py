"""Cache of raw PubTator documents, keyed on PMID.

Why raw documents and not `PaperResponse`: ingestion stores the verbatim
upstream document (`persist_paper(session, paper, raw)`), so a cache holding
only the parsed model would warm nothing ingestion can use. The raw document
also parses identically under either `include_ref_passages` setting, so one
entry serves both callers.

Every method is fail-open. A cache miss, a timeout, a corrupt value and an
unreachable Redis are all the same outcome to a caller: fetch it from PubTator.
That is what keeps a broken cache from breaking ingest.
"""

from __future__ import annotations

import json
import logging
from typing import Iterable, Optional

from api.redis_conn import close_client, get_client

log = logging.getLogger(__name__)

#: Key prefix. Bare integers would work in a dedicated instance, but a prefix
#: keeps `--scan` legible and leaves room for other cached kinds later.
KEY_PREFIX = "doc:"


def _key(pmid: int) -> str:
    return f"{KEY_PREFIX}{int(pmid)}"


class DocumentCache:
    """Raw PubTator documents with a TTL. Never raises."""

    def __init__(self, url: str, ttl_seconds: int, timeout: float, enabled: bool = True) -> None:
        self._url = url
        self._ttl = ttl_seconds
        self._timeout = timeout
        self._enabled = enabled

    def _client(self):
        return get_client(self._url, self._timeout) if self._enabled else None

    async def get_many(self, pmids: Iterable[int]) -> dict[int, dict]:
        """Cached documents, by PMID. Absent, unreadable and unparseable alike
        are simply missing from the result."""
        wanted = [int(p) for p in pmids]
        if not wanted:
            return {}
        client = self._client()
        if client is None:
            return {}

        try:
            raw_values = await client.mget([_key(p) for p in wanted])
        except Exception as exc:
            log.warning("document cache read failed (%s); falling through to PubTator", exc)
            return {}

        found: dict[int, dict] = {}
        for pmid, value in zip(wanted, raw_values):
            if value is None:
                continue
            try:
                found[pmid] = json.loads(value)
            except (ValueError, TypeError):
                # A corrupt entry is a miss, not an error. It will be
                # overwritten by the fetch this miss triggers.
                log.warning("discarding unreadable cache entry for PMID %s", pmid)
        return found

    async def get(self, pmid: int) -> Optional[dict]:
        return (await self.get_many([pmid])).get(int(pmid))

    async def set_many(self, documents: dict[int, dict]) -> int:
        """Store documents under their PMIDs. Returns how many were written."""
        if not documents:
            return 0
        client = self._client()
        if client is None:
            return 0

        try:
            pipe = client.pipeline(transaction=False)
            for pmid, document in documents.items():
                pipe.set(_key(pmid), json.dumps(document).encode(), ex=self._ttl)
            await pipe.execute()
        except Exception as exc:
            # A failed write must not fail the request that produced the data.
            log.warning("document cache write failed (%s); continuing uncached", exc)
            return 0
        return len(documents)

    async def aclose(self) -> None:
        """Release this loop's connection. Safe to call when there is none."""
        await close_client(self._url)

    async def delete(self, pmid: int) -> None:
        """Drop one document — called once it is safely in Postgres."""
        client = self._client()
        if client is None:
            return
        try:
            await client.delete(_key(pmid))
        except Exception as exc:
            log.debug("document cache delete failed for PMID %s (%s)", pmid, exc)
