"""Shared plumbing for the NCBI service clients — not a client itself.

`pb_client` (PubTator3) and `pm_client` (PMC Open Access) are separate
integrations with separate directories, but they talk to one organisation that
asks for one request budget. The rate limiter in `http.py` is a process-wide
singleton, so it has to live somewhere both can reach without either depending
on the other.
"""

from api.ncbi.errors import (
    InvalidRequestError,
    NcbiError,
    NotFoundError,
    UpstreamError,
)
from api.ncbi.http import build_client

__all__ = [
    "InvalidRequestError",
    "NcbiError",
    "NotFoundError",
    "UpstreamError",
    "build_client",
]
