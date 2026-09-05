# api/pm_client

Client for **PMC Open Access**, and the `/pm` route that exposes it. Downloads
an article's actual files — JATS XML, plain text, PDF, supplementary media — by
PMCID, into `<papers_dir>/<PMCID>/`.

Separate from `pb_client` because it is a separate service. PubTator is a
search API keyed on PMID; this is a plain S3 bucket with no query API, keyed on
PMCID. What they share — connection pool, rate limit, error types — is in
[`api/ncbi`](../ncbi).

## Files

| File | Purpose |
|---|---|
| `pmc.py` | `PmcClient`: metadata lookup, then the file downloads |
| `routes.py` | `/pm/download` |
| `models.py` | `DownloadResponse` / `DownloadedFile` — bucket JSON normalised |

## Worth knowing

**PubTator cannot serve this.** Its export endpoint rejects `pmcids` outright
(HTTP 400, "pmids is a mandatory parameter"), which is why downloads bypass it
entirely for the S3 bucket that replaced NCBI's retired FTP service in August
2026.

**Version fallback.** Almost every article is version 1, so the client tries
`metadata/<PMCID>.1.json` first and only falls back to an S3 listing when that
404s — one request in the common case instead of two.

**Downloads are serial, and stream to a `.part` file** that is renamed on
completion, so an interrupted download cannot be mistaken for a finished one.

**`normalise_pmcid` is the path guard.** Its value becomes a directory name, so
the regex that validates it is also what keeps writes inside `papers_dir`.

## Dependencies

`httpx`, `pydantic`, `fastapi`, `api.ncbi`. Configured from
`api.app.config.Settings` (`pmc_s3_base_url`, `papers_dir`).
