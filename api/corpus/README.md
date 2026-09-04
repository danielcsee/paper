# api/corpus

Reads over papers that finished importing.

```
GET /corpus?page=1&page_size=20   ->  CorpusPage        (the listing)
GET /corpus/rag_search?query=...  ->  RagSearchResponse (ranked papers)
GET /corpus/{paper_id}            ->  CorpusPaperDetail (one whole paper)
```

Read-only: `api.ingestion` writes these tables, this package reads them.

## Files

| File | Purpose |
|---|---|
| `routes.py` | The three GET routes: paging and validation |
| `queries.py` | The listing/detail reads, testable without HTTP |
| `rag.py` | Retrieval: score chunks, aggregate per paper, rank |
| `models.py` | Response models for all three routes |

## Decisions worth knowing

**"Imported" means the final stage is done.** Every route joins
`paper_stage_runs`, not `papers`: a row exists there from the moment `/import`
reserves one, long before the paper has content.

**`rag_search` is registered before `/corpus/{paper_id}`.** FastAPI matches in
order, and "rag_search" against an `int` path parameter is a 422.

**Retrieval only — no LLM.** Chunks below `RAG_SCORE_THRESHOLD` (0.55) are
dropped, survivors are summed per paper, and the top three come back with their
strongest excerpts. The scan is exhaustive: the HNSW index answers "nearest k",
which cannot express "all above a cutoff".

**The aggregator is a named function**, not an inline `sum()`. `sum` rewards
length: measured here the mean surviving score barely differs between papers,
so nearly all the separation comes from how *many* chunks survive.
`AGGREGATORS` also holds `max` and `mean`.

**Listing order is `finished_at DESC, id DESC`**; the id breaks ties so no
paper lands on two pages as the reader scrolls.

## Dependencies

`api.db`, `api.ingestion.embedding` (query vector), `fastapi`, `pydantic`.
