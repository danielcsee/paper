# api/ingestion

Selected search results go to `POST /import`, which queues one Celery chain per
paper, ending with that paper stored in Postgres, chunked and embedded.

```
POST /import  ->  chain(fetch_paper | persist_paper | embed_chunks)
```

**Status: partly built.** `routes.py`, `models.py`, `celery_app.py`, Redis,
settings and the embedding model are done. `tasks.py`, `persist.py`,
`chunking.py` and `embedding.py` keep real signatures and docstrings with
`NotImplementedError` bodies.

## Files

| File | Purpose |
|---|---|
| `celery_app.py` | Celery instance, serialization, rate limits |
| `tasks.py` | The three chain tasks |
| `persist.py` | `PaperResponse` → rows, plus the ledger reads `/import` uses |
| `chunking.py` | Passages → chunks. Pure functions, no network or DB |
| `embedding.py` | Lazily-loaded sentence-transformers model |
| `routes.py` | `POST /import`, `GET /import/status` |
| `models.py` | Request/response models for those routes |

## Decisions worth knowing

**Tasks pass a `paper_id`, never a payload.** A PubTator document is ~130KB;
through the broker every message would be huge. Stage one writes it to
`paper_pubtator_docs`, so later stages re-run from stored state.

**Embedding is its own stage** — the slow part, retryable without re-fetching.

**`fetch_paper` calls `PubTatorClient` directly**, not our `/pb/paper` route.

**Both routes are sync `def`**, so blocking SQLAlchemy runs in FastAPI's
threadpool. `/import` returns a job per paper: `rejected` (no PMID),
`already_imported`, `in_progress` (a chain is still on it — Celery does not
deduplicate), or `queued`. `force` overrides both ledger states.

## Dependencies

`celery` + Redis, `sentence-transformers` with **BAAI/bge-base-en-v1.5**
(768-d, matching `EMBEDDING_DIM`), `api.pb_client`, `api.db`. Worker:
[`scripts/dev.sh`](../../scripts).
