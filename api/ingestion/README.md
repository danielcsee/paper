# api/ingestion

Selected search results go to `POST /import`, which queues one Celery chain per
paper, ending with that paper stored in Postgres, chunked and embedded.

```
POST /import  ->  chain(ingest_paper | embed_paper)
```

## Files

| File | Purpose |
|---|---|
| `celery_app.py` | Celery instance, serialization, rate limits |
| `tasks.py` | The two chain tasks |
| `persist.py` | `PaperResponse` → rows, plus the ledger reads |
| `chunking.py` | Passages → chunks. Pure: no network, DB or torch |
| `embedding.py` | Lazily-loaded sentence-transformers model |
| `routes.py` | `POST /import`, `GET /import/status` |
| `models.py` | Models for those routes |

## Decisions worth knowing

**Two stages, split by retry cost.** Ingest is bound by NCBI's ~3 req/s and
costs another fetch to retry; embedding is CPU-bound and free to retry locally.
Chunking rides with ingest: chunks are 1:1 with PubTator passages.

**Tasks pass a `paper_id`, never a payload.** The fetched document is ~130KB
and one paper's vectors larger still; both stay in Postgres.

**The worker needs a non-forking pool.** On macOS the encoder selects Metal
(`mps`), which cannot be initialised in a forked child, so prefork dies with
SIGABRT. `dev.sh` uses `--pool=solo`; set `embedding_device=cpu` for prefork.

**`/import` marks stages pending before queueing, but never over a `done`
row** — that would defeat the fingerprint skip and re-fetch a paper we hold.

## Dependencies

`celery` + Redis, `sentence-transformers` with **BAAI/bge-base-en-v1.5**
(768-d, matching `EMBEDDING_DIM`), `api.pb_client`, `api.db`. Worker:
[`scripts/dev.sh`](../../scripts).
