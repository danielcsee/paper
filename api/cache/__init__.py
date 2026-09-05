"""Redis-backed caches. Currently one: raw PubTator documents by PMID."""

from api.cache.documents import DocumentCache

__all__ = ["DocumentCache"]
