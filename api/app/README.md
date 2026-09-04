# api/app

The FastAPI application itself: configuration, startup wiring, and static
hosting of the compiled UI. No domain logic lives here — that is in
[`pb_client/`](../pb_client) and [`db/`](../db).

## Files

**`config.py`** — a single `Settings` model (pydantic-settings) read from the
environment or the repository-root `.env`. It holds the datastore URLs, the
NCBI base URLs, the download directory, and the HTTP timeout. `get_settings()`
is `lru_cache`d, so settings are read once per process.

`ncbi_contact_email` is unset by default and nothing is sent to NCBI unless you
supply one yourself.

**`main.py`** — builds the app. The `lifespan` handler creates one pooled
`httpx.AsyncClient` for the process and constructs the `PubTatorClient` and
`PmcClient` onto `app.state`, closing the client on shutdown. Sharing one pool
across both clients is deliberate: NCBI asks callers to stay under roughly
three requests per second, which is easier to honour from a single pool.

Route order matters here. The `/pb` router is included *before* `StaticFiles`
is mounted at `/`, because a root mount otherwise swallows every path. When no
built UI bundle is present, `/` returns a 503 explaining how to build it rather
than a bare 404.

## Dependencies

`fastapi`, `pydantic-settings`, `httpx`. Imports `api.pb_client` for the
clients and router.

## Configuration

See `.env.example` at the repository root for the recognised variables.
