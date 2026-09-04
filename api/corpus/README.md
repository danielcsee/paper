# api/corpus

The user's corpus: every paper that finished importing.

```
GET /corpus?page=1&page_size=20   ->  CorpusPage        (the listing)
GET /corpus/{paper_id}            ->  CorpusPaperDetail (one whole paper)
```

Read-only: `api.ingestion` writes these tables, this package reads them.

## Files

| File | Purpose |
|---|---|
| `routes.py` | The two GET routes: paging and validation |
| `queries.py` | The reads, testable without HTTP |
| `models.py` | `CorpusPaper`, `CorpusPage`, `CorpusPaperDetail` |

## Decisions worth knowing

**"Imported" means the final stage is done.** Both routes join
`paper_stage_runs` rather than reading `papers`: a `papers` row exists from the
moment `/import` reserves one, long before the paper has content.

**Ordering is `finished_at DESC, id DESC`.** The id breaks ties: without a
total order, two papers finishing in the same instant could appear on two pages
or on neither as the reader scrolls.

**`chunk_type` is what makes the reader possible.** `section_type` alone cannot
tell a heading ("Background") from body text; PubTator's passage `type` can.

**Authors, snippets and chunk counts are one query each per page**, not one per
paper.

**A page past the end returns an empty list, not a 404.** Running off the end
is normal for an infinite scroll. The detail route *does* 404, for a paper
absent *or* unfinished — a reader must not get a half-ingested document.

`CorpusPaper` mirrors the fields a search result exposes, so the frontend
renders both with the same `PaperCard`.

## Dependencies

`api.db` for the models and session, `fastapi`, `pydantic`. Nothing here
reaches the network.
