# api/ingestion

`POST /import` queues one Celery chain per paper, ending with it stored in
Postgres, chunked and embedded. `GET /import/status` reports progress.

```
chain(ingest_paper | embed_paper)
```

## Files

| File | Purpose |
|---|---|
| `celery_app.py` | Celery instance, serialization, rate limits |
| `tasks.py` | The two chain tasks |
| `persist.py` | `PaperResponse` → rows, and the ledger reads |
| `chunking.py` | Passages → chunks. Pure: no network, DB or torch |
| `embedding.py` | Lazy model; document and query encoders |
| `routes.py` | The two routes |
| `models.py` | Their models |

## Decisions worth knowing

**Two stages, split by retry cost.** Ingest is bound by NCBI's ~3 req/s and
costs another fetch to retry; embedding is CPU-bound and retries locally.
Chunking rides with ingest: chunks are 1:1 with passages.

**Tasks pass a `paper_id`, never a payload** — the document is ~130KB and the
vectors larger still, so both stay in Postgres.

**The worker needs a non-forking pool.** On macOS the encoder selects Metal,
which cannot initialise in a forked child, so prefork dies with SIGABRT
(`dev.sh` uses `--pool=solo`).

**`/import` marks stages pending before queueing, never over a `done` row**,
which would defeat the fingerprint skip and re-fetch a paper we hold.

**`/import/status` returns a derived `state`** (queued/started/success/error)
beside the raw rows, so "which stage is last" stays a backend fact. Failure is
checked first, so a late failure is an error, not a success.

## Dependencies

`celery` + Redis, `sentence-transformers`, `api.pb_client`, `api.ncbi`,
`api.cache`, `api.db`.
Worker: [`scripts/dev.sh`](../../scripts).
