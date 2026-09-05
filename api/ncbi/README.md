# api/ncbi

Shared plumbing for the NCBI clients — not a client itself, and it knows
nothing about papers.

`pb_client` (PubTator3) and `pm_client` (PMC Open Access) are separate
integrations, but one organisation with one request budget. This holds what
neither should own alone, so neither imports the other.

## Files

| File | Purpose |
|---|---|
| `http.py` | `build_client` (pooled, polite, rate limited), `get` (retry/backoff) |
| `errors.py` | `NcbiError` and friends, carrying the status routes return |

## The rate limit

NCBI rate-limits its services as a whole — ~3 requests/second — so PubTator and
PMC share one budget: one client within a process, one Redis key across them.

`build_client` installs it as a custom `transport=` — httpx has no rate
parameter, and `limits=` caps connections, not rate. The transport, not `get()`,
because `pm_client` streams downloads straight off the `AsyncClient` and
redirects turn one call into several.

**`RedisRateLimiter`** is the real one. A Lua script claims the next free slot
atomically and returns the wait; the caller sleeps outside any lock. Time comes
from Redis's `TIME`, not the caller's — the worker and web process share no
clock. The key sits on the **broker's** Redis: ~50 bytes on an instance running
`noeviction`, so it cannot be evicted and needs no container of its own.

**`RateLimiter`** is the fallback when Redis is unreachable, degrading to 3/s
*per process* rather than dropping the limit. It must stay a module-level
singleton: the worker builds a client per task, so a per-client fallback would
enforce nothing across them.

## Dependencies

`httpx`, `redis` via `api.redis_conn`.
