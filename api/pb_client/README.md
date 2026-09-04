# api/pb_client

Clients for the two NCBI services this project reads from, plus the `/pb` HTTP
routes that expose them. They live together because they are two halves of one
job — find a paper, then fetch it — but they are separate services with
separate constraints.

## Files

| File | Purpose |
|---|---|
| `pubtator.py` | PubTator3: text search, and full annotated papers |
| `pmc.py` | PMC Open Access S3: downloads the article files by PMCID |
| `routes.py` | `/pb/search`, `/pb/download`, `/pb/paper` |
| `models.py` | Our response models — upstream JSON normalised, not mirrored |
| `http.py` | One connection pool, NCBI-polite headers, retry/backoff |
| `errors.py` | Exceptions carrying the HTTP status routes should return |

## Two constraints worth knowing

**PubTator is keyed on PMID.** Passing `pmcids=` alone is rejected with HTTP
400. This costs nothing in practice: a paper has full text in PubTator exactly
when it is also in PMC, and search returns both ids.

**Search ignores `page_size`** — upstream always returns 10 results per page —
so `search()` exposes `page` only and reports the size that came back.

Downloads bypass PubTator and go to the S3 bucket that replaced NCBI's retired
FTP service in August 2026.

## Dependencies

`httpx` for transport, `pydantic` for the models, `fastapi` for the router.
Configured from `api.app.config.Settings`.

## Notes

`Annotation.grounded` is `False` when upstream returned identifier `"-"`. These
are kept rather than dropped so callers decide; ingestion discards them, since
ungrounded mentions fragment the graph.
