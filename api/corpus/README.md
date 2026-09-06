# api/corpus

Reads over papers that finished importing.

```
GET /corpus?page=1&page_size=20   ->  CorpusPage        (the listing)
GET /corpus/rag_search?query=...  ->  RagSearchResponse (ranked papers)
GET /corpus/{paper_id}            ->  CorpusPaperDetail (one whole paper)
GET /corpus/{paper_id}/references ->  ReferenceList     (importable refs)
GET /corpus/{paper_id}/entities   ->  PaperEntityList   (concepts + spans)
```

Read-only: `api.ingestion` writes these tables, this package reads them.

## Files

| File | Purpose |
|---|---|
| `routes.py` | The four GET routes: paging and validation |
| `queries.py` | Listing, detail and reference reads |
| `rag.py` | Retrieval: score chunks, aggregate per paper, rank |
| `models.py` | Response models for all four |

## Decisions worth knowing

**"Imported" means the final stage is done.** Routes join `paper_stage_runs`,
not `papers`: a row exists there from the moment `/import` reserves one.

**Route order matters.** `rag_search` is registered before
`/corpus/{paper_id}`, since FastAPI matches in order and "rag_search" against
an `int` path parameter is a 422.

**`/references` must request full text.** The light export reports
`pmcid: null` even for papers that *are* in PMC — it only appears when full
text is actually returned — so nothing cheaper can decide whether a reference
exists as a full Paper. A reference qualifies on `pmcid` **and** body passages.
Only references carrying a PMID can be asked about at all (0–93% of them,
measured). Everything returned is importable, so the UI's count is exact.

**Retrieval is LLM-free.** Chunks below `RAG_SCORE_THRESHOLD` are dropped,
survivors summed per paper, top three returned with their best excerpts. The
aggregator is a named function; `AGGREGATORS` also holds `max` and `mean`.

## Dependencies

`api.db`, `api.pb_client`, `api.ncbi`, `api.ingestion.embedding`, `fastapi`, `pydantic`.
