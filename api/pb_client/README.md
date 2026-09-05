# api/pb_client

Client for **PubTator3**, and the `/pb` routes that expose it. Two endpoints:
text search over the annotated literature, and the export that returns one
paper's full annotated text structured into passages.

Downloading an article's *files* is a separate service, in
[`api/pm_client`](../pm_client). What they share — connection pool, rate limit,
error types — is in [`api/ncbi`](../ncbi).

## Files

| File | Purpose |
|---|---|
| `pubtator.py` | `PubTatorClient`: search, and full annotated papers |
| `routes.py` | `/pb/search`, `/pb/paper` |
| `models.py` | Our response models — upstream JSON normalised, not mirrored |

## Constraints

**Everything is keyed on PMID.** `pmcids=` alone is rejected with HTTP 400. A
paper has full text exactly when it is also in PMC, and search returns both ids,
so there is never anything to resolve.

**`full=true` is not optional when you need `pmcid`.** Measured: the light
export reports `pmcid: null` even for papers that *are* in PMC, because the
field only appears when full text is actually returned.

**Search ignores `page_size`** — upstream always returns 10 per page — so
`search()` exposes `page` only, reporting the size that came back.

`fetch_papers` chunks at 100 ids per request, the export endpoint's cap.

## Dependencies

`httpx`, `pydantic`, `fastapi`, `api.ncbi`. Configured from
`api.app.config.Settings`.

## Notes

`Annotation.grounded` is `False` when upstream returned identifier `"-"`. Kept,
not dropped, so callers decide: ingestion drops them, since ungrounded mentions
fragment the graph.
