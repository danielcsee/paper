"""Client for PMC Open Access — the article files themselves, keyed by PMCID.

Separate from `pb_client` because it is a separate service: a plain S3 bucket
with no query API, reached by PMCID, where PubTator is a search service reached
by PMID. What they share — connection pool, rate limit, error types — lives in
`api.ncbi`.
"""

from api.pm_client.models import DownloadedFile, DownloadResponse, FileKind
from api.pm_client.pmc import ALL_KINDS, PmcClient, normalise_pmcid
from api.pm_client.routes import router

__all__ = [
    "ALL_KINDS",
    "DownloadResponse",
    "DownloadedFile",
    "FileKind",
    "PmcClient",
    "normalise_pmcid",
    "router",
]
