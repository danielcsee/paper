# api/cache

Raw PubTator documents, cached in Redis by PMID for 24 hours.

`/corpus/{id}/references` already fetches every reference in full to decide
which are importable. Without a cache, importing them fetches the same
documents a second time. This closes that gap: the endpoint's fetch warms the
cache, and ingest reads it.

## Files

| File | Purpose |
|---|---|
| `client.py` | One async Redis connection per event loop, keyed by URL |
| `documents.py` | `DocumentCache`: get/get_many/set_many/delete, all fail-open |

## Four decisions

**Its own Redis instance** (`redis-cache`, port 6380), not the broker's.
`maxmemory` and eviction are per-instance, not per-database, and Celery's queue
lists carry no TTL — so a cache sharing the broker could evict queued tasks
silently. Isolated, `allkeys-lru` under a 1GB cap can only discard documents.

**Raw documents, not `PaperResponse`.** Ingestion persists the verbatim upstream
document, so caching only the parsed model would warm nothing it can use. Raw
also parses identically under either `include_ref_passages`, so one entry serves
both callers.

**Full text only.** Not "has passages" — every document has those; an
abstract-only record has exactly two and no `section_type`. The test is
`PaperResponse.importable`, the same predicate the references endpoint uses, so
what gets cached is exactly what gets displayed as importable.

**Fail-open everywhere.** A miss, a timeout, a corrupt value and an unreachable
Redis are one outcome to the caller: fetch from PubTator. Writes fail silently
too — a cache write must never fail the request that produced the data.

## Dependencies

`redis` (asyncio client). Configured from `api.app.config.Settings`.
