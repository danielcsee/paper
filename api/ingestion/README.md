# api/ingestion

The paper import pipeline. The sidebar's selected results go to
`POST /import`, which queues one Celery chain per paper, and the chain ends with that
paper stored in Postgres, chunked and embedded.

```
POST /import  ->  chain(fetch_paper | persist_paper | embed_chunks)
```

**Status: stubbed.** Every module has its real signatures, types and
docstrings; the bodies raise `NotImplementedError`. The Celery app, settings,
Redis service and embedding model are real and working.

## Files

| File | Purpose |
|---|---|
| `celery_app.py` | Celery instance, serialization, rate limits |
| `tasks.py` | The three chain tasks |
| `persist.py` | `PaperResponse` → rows; testable without a broker |
| `chunking.py` | Passages → chunks. Pure functions, no network or DB |
| `embedding.py` | Lazily-loaded sentence-transformers model |
| `routes.py` | `POST /import`, `GET /import/status` |
| `models.py` | Request/response models for those routes |

## Three decisions worth knowing

**Tasks pass a `paper_id`, never a payload.** A PubTator document is ~130KB;
moving it through the broker would make every message huge. Stage one writes it
to `paper_pubtator_docs`, so later stages re-run from stored state.

**Embedding is its own stage** — the slow, failure-prone part, retryable
without re-fetching from a rate-limited API.

**`fetch_paper` calls `PubTatorClient` directly**, not our own `/pb/paper`
route: same code path, no extra hop, no dependency on the web process.

## Dependencies

`celery` + Redis (broker and result backend), `sentence-transformers` with
**BAAI/bge-base-en-v1.5** (768-d, matching `api.db.models.EMBEDDING_DIM`),
`api.pb_client` to fetch and `api.db` to store.

Run the worker via [`scripts/dev.sh`](../../scripts).
