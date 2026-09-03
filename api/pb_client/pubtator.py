"""PubTator3 search.

    GET https://www.ncbi.nlm.nih.gov/research/pubtator3-api/search/?text=<query>&page=<n>

Verified against the live API: `page_size` is NOT honoured — the service always
returns 10 results per page — so this client exposes `page` only and reports the
page size that came back rather than one the caller asked for.
"""

from __future__ import annotations

import re

import httpx

from api.pb_client import http
from api.pb_client.errors import InvalidRequestError, UpstreamError
from api.pb_client.models import SearchResponse, SearchResult

#: PubTator marks up highlights as:
#:   "@GENE_BRCA1 @GENE_672 @@@<m>BRCA1</m>@@@ suppresses ..."
#: i.e. space-separated concept tags, then the matched span fenced in @@@ with
#: the query terms wrapped in <m>. Strip all of it for a displayable snippet.
_CONCEPT_TAG = re.compile(r"@[A-Z]+_\S+\s*")
_FENCE = re.compile(r"@@@")
_MARK = re.compile(r"</?m>")


def strip_highlight_markup(text_hl: str | None) -> str | None:
    if not text_hl:
        return None
    plain = _MARK.sub("", _FENCE.sub("", _CONCEPT_TAG.sub("", text_hl)))
    return re.sub(r"\s+", " ", plain).strip() or None


def _to_result(raw: dict) -> SearchResult:
    return SearchResult(
        pmid=raw.get("pmid"),
        pmcid=raw.get("pmcid"),
        title=raw.get("title"),
        journal=raw.get("journal"),
        authors=raw.get("authors") or [],
        date=raw.get("date"),
        doi=raw.get("doi"),
        score=raw.get("score"),
        text_hl=raw.get("text_hl"),
        snippet=strip_highlight_markup(raw.get("text_hl")),
    )


class PubTatorClient:
    def __init__(self, client: httpx.AsyncClient, base_url: str) -> None:
        self._client = client
        self._base_url = base_url.rstrip("/")

    async def search(self, text: str, page: int = 1) -> SearchResponse:
        query = text.strip()
        if not query:
            raise InvalidRequestError("text must not be empty")
        if page < 1:
            raise InvalidRequestError("page must be >= 1")

        response = await http.get(
            self._client, f"{self._base_url}/search/", params={"text": query, "page": page}
        )
        if response.status_code != 200:
            raise UpstreamError(
                f"PubTator search returned HTTP {response.status_code}: "
                f"{response.text[:200]}"
            )
        try:
            payload = response.json()
        except ValueError as exc:
            raise UpstreamError(f"PubTator search returned non-JSON: {exc}") from exc

        return SearchResponse(
            query=query,
            # Trust upstream's own view of where we are; asking for page 9e9
            # returns an empty page rather than an error.
            page=payload.get("current", page),
            page_size=payload.get("page_size", 0),
            total_results=payload.get("count", 0),
            total_pages=payload.get("total_pages", 0),
            results=[_to_result(r) for r in payload.get("results", [])],
        )
