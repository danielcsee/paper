"""PubTator3: text search, and full annotated papers.

    GET .../pubtator3-api/search/?text=<query>&page=<n>
    GET .../pubtator3-api/publications/export/biocjson?pmids=<id>&full=true

Two verified constraints shape this module:

* `page_size` is NOT honoured by search — the service always returns 10 results
  per page — so `search()` exposes `page` only and reports the size that came
  back rather than one the caller asked for.
* The export endpoint is keyed on **PMID**. Passing `pmcids=` alone is rejected
  with HTTP 400 ("pmids is a mandatory parameter"). This is not a limitation in
  practice: a paper has full text in PubTator exactly when it is also in PMC,
  and every such paper carries both ids in its search result.
"""

from __future__ import annotations

import json
import re
from typing import Iterable, Optional, Sequence

import httpx

from api.cache import DocumentCache
from api.ncbi import http
from api.ncbi.errors import InvalidRequestError, NotFoundError, UpstreamError
from api.pb_client.models import (
    Annotation,
    Author,
    PaperResponse,
    Passage,
    Reference,
    Relation,
    SearchResponse,
    SearchResult,
    normalise_identifier,
)

#: Hard server-side cap on ids per export request; 101 is rejected with a 400.
EXPORT_BATCH_LIMIT = 100

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
    def __init__(
        self,
        client: httpx.AsyncClient,
        base_url: str,
        cache: Optional["DocumentCache"] = None,
    ) -> None:
        self._client = client
        self._base_url = base_url.rstrip("/")
        self._cache = cache

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

    async def fetch_paper(
        self, pmid: int, *, full: bool = True, include_ref_passages: bool = False
    ) -> PaperResponse:
        """Fetch one paper's annotated text, structured into passages."""
        paper, _ = await self.fetch_paper_with_raw(
            pmid, full=full, include_ref_passages=include_ref_passages
        )
        return paper

    async def fetch_papers(
        self, pmids: Sequence[int], *, full: bool = True
    ) -> list[PaperResponse]:
        """Fetch many papers, in the order asked for.

        The export endpoint takes at most `EXPORT_BATCH_LIMIT` ids per call, so
        longer lists are chunked. `full=True` is not optional for anything that
        needs `pmcid`: measured, the light response reports `pmcid: null` even
        for papers that are in PMC, because it only appears when full text is
        actually returned.

        Cached documents are served without a request, and only the misses are
        asked for — which also shrinks the batches. Ordering follows the ids
        given rather than the upstream response, since with a partly warm cache
        there is no single upstream response to follow.
        """
        unique = list(dict.fromkeys(int(pmid) for pmid in pmids))
        if not unique:
            return []

        documents = await self._cached(unique, full=full)
        missing = [pmid for pmid in unique if pmid not in documents]

        fetched: list[dict] = []
        for start in range(0, len(missing), EXPORT_BATCH_LIMIT):
            batch = missing[start : start + EXPORT_BATCH_LIMIT]
            params = {"pmids": ",".join(str(pmid) for pmid in batch)}
            if full:
                params["full"] = "true"
            response = await http.get(
                self._client, f"{self._base_url}/publications/export/biocjson", params=params
            )
            if response.status_code != 200:
                raise UpstreamError(
                    f"PubTator export returned HTTP {response.status_code}: "
                    f"{response.text[:200]}"
                )
            fetched.extend(_parse_documents(response.text))

        papers = [_to_paper(doc, include_ref_passages=False) for doc in fetched]
        await self._remember(zip(papers, fetched), full=full)

        by_pmid = {paper.pmid: paper for paper in papers if paper.pmid is not None}
        by_pmid.update(
            {pmid: _to_paper(doc, include_ref_passages=False) for pmid, doc in documents.items()}
        )
        # Upstream silently omits ids it has no record of; so does this.
        return [by_pmid[pmid] for pmid in unique if pmid in by_pmid]

    async def fetch_paper_with_raw(
        self, pmid: int, *, full: bool = True, include_ref_passages: bool = False
    ) -> tuple[PaperResponse, dict]:
        """Like `fetch_paper`, but also returns the verbatim upstream document.

        One request, both representations. Ingestion stores the raw document so
        later stages can re-run locally, and asking twice would double our load
        on a service that tolerates ~3 requests/second.
        """
        cached = await self._cached([pmid], full=full)
        if pmid in cached:
            raw = cached[pmid]
            return _to_paper(raw, include_ref_passages=include_ref_passages), raw

        params = {"pmids": str(pmid)}
        if full:
            params["full"] = "true"
        response = await http.get(
            self._client, f"{self._base_url}/publications/export/biocjson", params=params
        )
        if response.status_code == 404:
            raise NotFoundError(f"PubTator has no record for PMID {pmid}")
        if response.status_code != 200:
            raise UpstreamError(
                f"PubTator export returned HTTP {response.status_code}: {response.text[:200]}"
            )

        documents = _parse_documents(response.text)
        if not documents:
            raise NotFoundError(f"PubTator has no record for PMID {pmid}")
        raw = documents[0]
        paper = _to_paper(raw, include_ref_passages=include_ref_passages)
        await self._remember([(paper, raw)], full=full)
        return paper, raw

    # -- cache -----------------------------------------------------------------
    # Only full-text documents are cached, and only `full=True` reads it. An
    # abstract-only record cannot be ingested, so caching one would spend the
    # budget on the majority case that never pays it back; and serving a full
    # document to a `full=False` caller would quietly return more than asked.

    async def _cached(self, pmids: Sequence[int], *, full: bool) -> dict[int, dict]:
        if self._cache is None or not full:
            return {}
        return await self._cache.get_many(pmids)

    async def _remember(
        self, pairs: Iterable[tuple[PaperResponse, dict]], *, full: bool
    ) -> None:
        if self._cache is None or not full:
            return
        keep = {
            paper.pmid: raw
            for paper, raw in pairs
            if paper.pmid is not None and paper.importable
        }
        await self._cache.set_many(keep)

# --------------------------------------------------------------------------
# Full paper
# --------------------------------------------------------------------------

#: Passages that are bibliography entries. They are 30-40% of all passages and
#: are surfaced as `references` instead, so they are kept out of `passages` by
#: default — no data is lost either way.
REF_SECTION = "REF"

#: Section types that mean the response carried body text, not just the stub.
_STUB_SECTIONS = {"TITLE", "ABSTRACT", None}


def _parse_names(infons: dict) -> list[Author]:
    """Authors arrive as name_0..name_N = "surname:Adachi;given-names:Jonathan D"."""
    authors: list[tuple[int, Author]] = []
    for key, value in infons.items():
        if not key.startswith("name_") or not isinstance(value, str):
            continue
        try:
            index = int(key[len("name_") :])
        except ValueError:
            continue
        parts = dict(
            piece.split(":", 1) for piece in value.split(";") if ":" in piece
        )
        authors.append(
            (index, Author(surname=parts.get("surname"), given_names=parts.get("given-names")))
        )
    return [author for _, author in sorted(authors, key=lambda pair: pair[0])]


def _to_annotation(raw: dict) -> Optional[Annotation]:
    locations = raw.get("locations") or []
    if not locations:
        return None
    infons = raw.get("infons") or {}
    identifier = infons.get("identifier")
    # `bool(" ")` is True, so the old `identifier != "-"` test let a
    # whitespace-only id through as grounded and into the entities table.
    grounded_id = normalise_identifier(identifier, infons.get("database"))
    normalized = infons.get("normalized_id")
    return Annotation(
        id=raw.get("id"),
        type=infons.get("type"),
        text=raw.get("text"),
        offset=locations[0].get("offset", 0),
        length=locations[0].get("length", 0),
        # Normalised when usable; otherwise the raw value is kept, because
        # ungrounded annotations are surfaced rather than dropped here.
        identifier=grounded_id
        if grounded_id is not None
        else (None if identifier is None else str(identifier)),
        # Upstream returns str for MeSH, int for NCBI taxonomy.
        normalized_id=None if normalized is None else str(normalized),
        database=infons.get("database"),
        name=infons.get("name"),
        biotype=infons.get("biotype"),
        grounded=grounded_id is not None,
    )


def _to_relation(raw: dict) -> Relation:
    """Verified wire shape:

        {"id": "R1", "nodes": [],
         "infons": {"type": "Association", "score": "0.9994",
                    "role1": {...}, "role2": {...}}}

    Note everything lives under `infons` — including `type` and `score`.
    """
    infons = raw.get("infons") or {}
    role1 = infons.get("role1") or {}
    role2 = infons.get("role2") or {}
    score = infons.get("score")
    try:
        score = float(score) if score is not None else None
    except (TypeError, ValueError):
        score = None
    return Relation(
        id=raw.get("id"),
        type=infons.get("type"),
        score=score,
        role1_identifier=role1.get("identifier"),
        role1_name=role1.get("name"),
        role1_type=role1.get("type"),
        role2_identifier=role2.get("identifier"),
        role2_name=role2.get("name"),
        role2_type=role2.get("type"),
    )


def _parse_documents(body: str) -> list[dict]:
    """The export endpoint answers with either a JSON object or one JSON
    document per line, depending on how many ids were requested."""
    body = body.strip()
    if not body:
        return []
    try:
        payload = json.loads(body)
    except ValueError:
        documents = []
        for line in body.splitlines():
            line = line.strip()
            if line:
                try:
                    documents.append(json.loads(line))
                except ValueError as exc:
                    raise UpstreamError(f"PubTator export returned non-JSON: {exc}") from exc
        return documents

    if isinstance(payload, dict):
        if "PubTator3" in payload:
            return list(payload["PubTator3"])
        return [payload]
    if isinstance(payload, list):
        # An error body is a bare list of strings, e.g. ["pmids is a mandatory
        # parameter."]; a success never is.
        if payload and isinstance(payload[0], str):
            raise UpstreamError(f"PubTator export rejected the request: {payload[0]}")
        return list(payload)
    raise UpstreamError("PubTator export returned an unexpected payload")


def _to_paper(doc: dict, *, include_ref_passages: bool) -> PaperResponse:
    raw_passages = doc.get("passages") or []

    # The `front` passage carries the article-level record: ids, journal title,
    # year/volume/pages, and the full author list as name_0..name_N.
    front: dict = {}
    for passage in raw_passages:
        infons = passage.get("infons") or {}
        if infons.get("type") == "front" or infons.get("section_type") == "TITLE":
            front = infons
            break

    authors = _parse_names(front)
    if not authors:
        # Fall back to the flat top-level list ("Adachi JD").
        authors = [Author(surname=name) for name in (doc.get("authors") or [])]

    passages: list[Passage] = []
    references: list[Reference] = []
    section_counts: dict[str, int] = {}

    for index, raw in enumerate(raw_passages):
        infons = raw.get("infons") or {}
        section = infons.get("section_type")
        section_counts[section or "UNKNOWN"] = section_counts.get(section or "UNKNOWN", 0) + 1

        if section == REF_SECTION:
            references.append(
                Reference(
                    ordinal=len(references),
                    title=raw.get("text") or None,
                    pmid=infons.get("pub-id_pmid"),
                    doi=infons.get("pub-id_doi"),
                    source=infons.get("source"),
                    year=infons.get("year"),
                    volume=infons.get("volume"),
                    fpage=infons.get("fpage"),
                    lpage=infons.get("lpage"),
                    authors=_parse_names(infons),
                )
            )
            if not include_ref_passages:
                continue

        annotations = [
            annotation
            for annotation in (_to_annotation(a) for a in raw.get("annotations") or [])
            if annotation is not None
        ]
        passages.append(
            Passage(
                ordinal=index,
                section_type=section,
                type=infons.get("type"),
                offset=raw.get("offset", 0),
                text=raw.get("text") or "",
                annotations=annotations,
            )
        )

    # With full=true the title passage is section_type TITLE / type front; the
    # abstract-only response carries no section_type at all and types it "title".
    title = next(
        (
            p.text
            for p in passages
            if (p.section_type == "TITLE" and p.type == "front") or p.type == "title"
        ),
        None,
    )
    pmid = doc.get("pmid")

    return PaperResponse(
        pmcid=doc.get("pmcid"),
        pmid=int(pmid) if pmid is not None else None,
        doi=front.get("article-id_doi"),
        title=title,
        journal=doc.get("journal"),
        journal_title=front.get("journal-title"),
        date=doc.get("date"),
        year=front.get("year"),
        volume=front.get("volume"),
        fpage=front.get("fpage"),
        lpage=front.get("lpage"),
        authors=authors,
        has_full_text=any(
            (p.get("infons") or {}).get("section_type") not in _STUB_SECTIONS
            for p in raw_passages
        ),
        passages=passages,
        relations=[_to_relation(r) for r in doc.get("relations") or []],
        references=references,
        section_counts=section_counts,
    )
