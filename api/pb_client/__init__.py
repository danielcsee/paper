"""Clients for the two NCBI services this project reads from.

PubTator3  — text search over the annotated literature      (pubtator.py)
PMC OA S3  — the article files themselves, keyed by PMCID   (pmc.py)

They live together because they are two halves of one job (find a paper, then
fetch it), but they are separate services with separate constraints.
"""

from api.pb_client.errors import (
    InvalidRequestError,
    NotFoundError,
    PbClientError,
    UpstreamError,
)
from api.pb_client.pmc import PmcClient
from api.pb_client.pubtator import PubTatorClient
from api.pb_client.routes import router

__all__ = [
    "InvalidRequestError",
    "NotFoundError",
    "PbClientError",
    "PmcClient",
    "PubTatorClient",
    "UpstreamError",
    "router",
]
