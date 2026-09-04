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
| [`pb_client/`](pb_client) | Clients for the two NCBI services, and the `/pb` routes |
| [`db/`](db) | SQLAlchemy models, session plumbing, and Alembic migrations |

## Dependencies

Declared in `requirements.txt`:

- **fastapi** / **uvicorn** — HTTP server
- **pydantic-settings** — configuration from environment and `.env`
- **httpx** — async HTTP client for the NCBI calls
- **SQLAlchemy** 2.x / **alembic** / **psycopg** (v3) / **pgvector** — Postgres
- **celery** — declared so the ingestion pipeline has a home; no tasks are
  wired up yet

Postgres and Neo4j themselves run in Docker (`docker-compose.yml` at the root).

## Notes

Nothing in the API connects to Postgres or Neo4j yet. `db/` defines the schema
and `app/config.py` holds the connection strings, but the request path today is
NCBI-only: search, download, and fetch an annotated paper.
