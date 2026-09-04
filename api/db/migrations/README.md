# api/db/migrations

The Alembic environment for the Postgres schema defined in
[`../models.py`](../models.py). `alembic.ini` at the repository root points
here via `script_location`.

## How it differs from a stock Alembic setup

**The database URL comes from the app, not `alembic.ini`.** `env.py` imports
`api.app.config.get_settings()` and runs the URL through
`api.db.base.normalise_url()`, so the application and its migrations cannot
disagree about which database is meant. The `sqlalchemy.url` key in
`alembic.ini` is intentionally not the source of truth.

**pgvector types are imported explicitly.** `env.py` imports `Vector` from
`pgvector.sqlalchemy` so autogenerate can render embedding columns; without
that import the generated migration references an unknown type.

## Usage

Applied automatically by [`scripts/dev.sh`](../../../scripts). To run by hand,
from the repository root:

```bash
./.venv/bin/alembic upgrade head              # apply
./.venv/bin/alembic revision --autogenerate -m "message"
./.venv/bin/alembic downgrade -1              # roll back one
```

Autogenerate compares `api.db.base.Base.metadata` against the live database, so
Postgres must be running — `docker compose up -d postgres`.

## Subdirectories

- `versions/` — the migration scripts themselves, applied in dependency order.
  Current head: `738f0cf2e8f0_initial_paper_schema`.

## Dependencies

`alembic`, `SQLAlchemy`, `pgvector`, `psycopg`. Imports from `api.app.config`
and `api.db.base`.
