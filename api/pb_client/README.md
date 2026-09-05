# api/pb_client

Clients for the two NCBI services this project reads from, plus the `/pb` routes
exposing them. Two halves of one job — find a paper, then fetch it — but
separate services, with separate constraints.

## Files

| File | Purpose |
|---|---|
| `pubtator.py` | PubTator3: text search, and full annotated papers |
| `pmc.py` | PMC Open Access S3: downloads article files by PMCID |
| `routes.py` | `/pb/search`, `/pb/download`, `/pb/paper` |
| `models.py` | Response models — upstream JSON normalised, not mirrored |
| `http.py` | One connection pool, polite headers, rate limit, retry/backoff |
| `errors.py` | Exceptions carrying the HTTP status routes return |

## Constraints

**NCBI tolerates ~3 requests/second.** `build_client` enforces it with a custom
`transport=` — httpx has no rate parameter, and `limits=` caps connections, not
rate. The transport catches what `get()` cannot: `pmc.py`'s streamed downloads,
and redirect hops. Its limiter is module-level: the worker builds a client
per paper, so a per-client one would reset each task.

**PubTator is keyed on PMID.** `pmcids=` alone is rejected with HTTP 400. A
paper has full text exactly when it is also in PMC, and search returns both ids.

**Search ignores `page_size`** — upstream always returns 10 per page — so
`search()` exposes `page` only, reporting the size that came back.

Downloads use the S3 bucket that replaced NCBI's FTP service (August 2026).

## Dependencies

`httpx` for transport, `pydantic` for models, `fastapi` for the router; all
configured from `api.app.config.Settings`.

## Notes

`Annotation.grounded` is `False` when upstream returned identifier `"-"`. Kept, not
dropped, so callers decide: ingestion drops them, since ungrounded mentions
fragment the graph.
