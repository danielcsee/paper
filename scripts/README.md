# scripts

Developer tooling. One script today.

## `dev.sh`

Launches the whole local stack — Postgres and Neo4j in Docker, the FastAPI
server and the React dev server on the host.

```bash
./scripts/dev.sh           # hot-reloading dev servers (UI on :5173)
./scripts/dev.sh --prod    # build the bundle and serve it from FastAPI (:8000)
./scripts/dev.sh --no-db   # skip Docker; app processes only
```

It is idempotent and safe to re-run. In order it will:

1. create `.env` from `.env.example` if missing, then source it
2. `docker compose up -d postgres neo4j` and wait on their healthchecks — the
   first run is slow, because Neo4j downloads the Graph Data Science plugin
3. create `.venv` if missing and install `api/requirements.txt`
4. apply Alembic migrations (`alembic upgrade head`)
5. `npm install` in `ui/` if `node_modules` is missing
6. start uvicorn, and Vite unless `--prod`

Ctrl-C stops the app processes. **The datastores keep running** — stop them
with `docker compose down`.

## Ports

Read from `.env`, with defaults: API `8000`, UI `5173`, Postgres `5432`, Neo4j
`7474` (browser) and `7687` (bolt).

## Dependencies

`bash`, `docker` (unless `--no-db`), `python3`, and `npm` — checked at startup
with a clear error rather than a stack trace.
