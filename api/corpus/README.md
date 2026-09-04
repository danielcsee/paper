# api/corpus

The user's corpus: every paper that finished importing.

```
GET /corpus?page=1&page_size=20   ->  CorpusPage
```

Read-only. `api.ingestion` writes these tables; this package only reads them.

## Files

| File | Purpose |
|---|---|
| `routes.py` | `GET /corpus` — paging and validation |
| `queries.py` | The reads, split out so they are testable without HTTP |
| `models.py` | `CorpusPaper`, `CorpusPage` |

## Decisions worth knowing

**"Imported" means the final stage is done.** The query joins
`paper_stage_runs` rather than reading `papers`, because a `papers` row exists
from the moment `/import` reserves one — long before the paper has content. A
queued, failed or half-imported paper is not in the corpus.

**Ordering is `finished_at DESC, id DESC`.** The id breaks ties: without a
total order, two papers finishing in the same instant could appear on two pages
or on neither as the reader scrolls.

**Authors, snippets and chunk counts are one query each per page**, not one per
paper. The snippet prefers an abstract chunk and falls back to the first chunk,
so a card looks the same as a search result.

**A page past the end returns an empty list, not a 404.** Running off the end
is normal for an infinite scroll, not an error.

`CorpusPaper` deliberately mirrors the fields a search result exposes, so the
frontend renders both with the same `PaperCard`.

## Dependencies

`api.db` for the models and session, `fastapi` for the router, `pydantic` for
the response models. Nothing here reaches the network.
