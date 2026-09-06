# api

The Python backend: a FastAPI server that fetches scientific papers from NCBI
and stores them in Postgres for retrieval.

It is a package rooted at the repository root, so imports and the ASGI target
are fully qualified — `uvicorn api.app.main:app`, not `main:app`. Run it via
[`scripts/dev.sh`](../scripts) rather than by hand; that script also starts the
datastores and applies migrations.

## Subdirectories

| Directory | Purpose |
|---|---|
| [`app/`](app) | FastAPI application object, lifespan wiring, and `Settings` |
| `redis_conn.py` | One async Redis connection per event loop, shared by both users |
| [`cache/`](cache) | Redis cache of raw PubTator documents, keyed by PMID |
| [`ncbi/`](ncbi) | Shared HTTP plumbing for the NCBI clients: pool, rate limit, errors |
| [`pb_client/`](pb_client) | PubTator3: search and full annotated papers (`/pb`) |
| [`pm_client/`](pm_client) | PMC Open Access: downloads article files (`/pm`) |
| [`db/`](db) | SQLAlchemy models, session plumbing, and Alembic migrations |
| [`ingestion/`](ingestion) | Celery import pipeline and the `/import` routes |
| [`corpus/`](corpus) | Read-only `/corpus` listing of imported papers |
| [`graph/`](graph) | Neo4j schema and the Postgres → knowledge-graph export |

## Dependencies

Declared in `requirements.txt`:

- **fastapi** / **uvicorn** — HTTP server
- **pydantic-settings** — configuration from environment and `.env`
- **httpx** — async HTTP client for the NCBI calls
- **SQLAlchemy** 2.x / **alembic** / **psycopg** (v3) / **pgvector** — Postgres
- **neo4j** — Bolt driver for the knowledge graph
- **celery[redis]** / **redis** — task queue, and the document cache
- **sentence-transformers** — local chunk embeddings (BAAI/bge-base-en-v1.5)

Postgres, Neo4j and two Redis instances — broker and document cache — run in
Docker (`docker-compose.yml` at the root).

## Notes

`/import` runs end to end: it fetches a paper from PubTator, stores it chunked
with its authors, references, entities, mentions and relations, then embeds
every chunk.

The graph is built separately, by [`scripts/build-graph.sh`](../scripts), which
projects those tables into Neo4j. It is not yet a stage in the import chain —
`paper_stage_runs` reserves `graph` for when it becomes one.
