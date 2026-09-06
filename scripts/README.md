# scripts

Developer tooling.

## `dev.sh`

Launches the whole local stack — Postgres and Neo4j in Docker, the FastAPI
server and the React dev server on the host.

```bash
./scripts/dev.sh           # hot-reloading dev servers (UI on :5173)
./scripts/dev.sh --prod    # build the bundle and serve it from FastAPI (:8000)
./scripts/dev.sh --no-db   # skip Docker; app processes only
./scripts/dev.sh --force   # start anyway, even if a copy is already running
```

**It refuses to become a second copy.** It asks `stop.sh --list` whether this
checkout is already running and stops if so. Duplicates do not announce
themselves: two uvicorns share `:8000` with the kernel quietly preferring the
older one, and two Celery workers turn every shared entity into a deadlock
candidate. Both look like application bugs.

It is idempotent and safe to re-run. In order it will:

1. create `.env` from `.env.example` if missing, then source it
2. `docker compose up -d postgres neo4j redis` and wait on their healthchecks — the
   first run is slow, because Neo4j downloads the Graph Data Science plugin
3. create `.venv` if missing and install `api/requirements.txt`
4. apply Alembic migrations (`alembic upgrade head`)
5. `npm install` in `ui/` if `node_modules` is missing
6. start uvicorn, and Vite unless `--prod`

Ctrl-C stops the app processes. **The datastores keep running** — stop them
with `docker compose down`.

## `build-graph.sh`

Projects the Postgres corpus into the Neo4j knowledge graph — see
[`api/graph`](../api/graph).

```bash
./scripts/build-graph.sh                 # apply schema, then load
./scripts/build-graph.sh --schema-only   # constraints and indexes only
./scripts/build-graph.sh --reset         # wipe nodes and edges, then reload
```

Idempotent: every node is merged on its key, so re-run it after importing more
papers. `--reset` clears data but keeps the constraints.

## `stop.sh`

Stops everything `dev.sh` starts — the host processes and this project's
containers.

```bash
./scripts/stop.sh              # app processes and datastores
./scripts/stop.sh --apps-only  # leave Postgres/Neo4j/Redis running
./scripts/stop.sh --dry-run    # list what would be stopped
```

It finds processes by *what they are* — uvicorn, Celery, Vite whose command line
or working directory is this checkout — not by who started them, so a server
launched by hand outside `dev.sh` is stopped too. That case is not hypothetical:
two uvicorns can hold `:8000` at once, one bound to `127.0.0.1` and one to
`0.0.0.0`, and the kernel prefers the specific bind, so the stale one keeps
serving `localhost` while a restart appears to have worked.

Afterwards it re-checks the API port and kills anything of ours still holding
it, which catches uvicorn's `--reload` child — its command line names neither
uvicorn nor the app, so no pattern matches it directly.

## `cleanup.sh`

```bash
./scripts/cleanup.sh            # this project's containers and orphans
./scripts/cleanup.sh --full     # everything; see below
./scripts/cleanup.sh --dry-run  # print the plan, change nothing
```

Default is scoped to this project and **keeps volumes**, so imported papers
survive.

`--full` removes **every container on this Docker host**, including other
Compose projects', then drops this project's volumes and images and deletes
`.venv/`, `ui/node_modules/` and `ui/dist/`. Volumes and images stay scoped to
this project — rebuilding this project should not delete another one's database.
It prints the foreign containers by name and requires you to type the project
name to continue; `--yes` skips that for scripted use.

## Ports

Read from `.env`, with defaults: API `8000`, UI `5173`, Postgres `5432`, Neo4j
`7474` (browser) and `7687` (bolt).

## Dependencies

`bash`, `docker` (unless `--no-db`), `python3`, and `npm` — checked at startup
with a clear error rather than a stack trace.
