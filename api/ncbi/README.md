# api/ncbi

Shared plumbing for the NCBI service clients. Not a client itself: it owns no
endpoint and knows nothing about papers.

`pb_client` (PubTator3) and `pm_client` (PMC Open Access) are separate
integrations, but one organisation with one request budget. This holds what
neither should own alone, so neither imports the other.

## Files

| File | Purpose |
|---|---|
| `http.py` | `build_client` (pooled, polite, rate limited) and `get` (retry/backoff) |
| `errors.py` | `NcbiError` and friends, carrying the HTTP status routes return |

## The rate limit

NCBI tolerates ~3 requests/second. `build_client` enforces it with a custom
`transport=`; httpx has no rate parameter, and `limits=` caps connections, not
rate. It belongs in the transport because `get()` is not the only way out of a
client — `pm_client` streams downloads directly off the `AsyncClient`, and
redirects turn one call into several.

The limiter is a **module-level singleton**, so every client this process builds
draws on one budget. Two things depend on that: `main.py` hands one
`AsyncClient` to both service clients, and the Celery worker builds a fresh
client inside a fresh `asyncio.run` for every paper — a per-client limiter would
reset each task and enforce nothing across them.

Slots are spaced 1/rate apart rather than bucketed, so no two requests leave
together. The lock is a `threading.Lock`, not an `asyncio.Lock`: on Python 3.9
asyncio primitives bind to the creating loop, which a limiter shared across
per-task loops cannot do. It is never held across an `await`.

## Dependencies

`httpx`. Nothing else — deliberately, including no config import.
