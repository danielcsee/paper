# api/app

The FastAPI application itself: configuration, startup wiring, and static
hosting of the compiled UI. No domain logic lives here — that is in
[`pb_client/`](../pb_client), [`pm_client/`](../pm_client) and [`db/`](../db).

## Files

**`config.py`** — a single `Settings` model (pydantic-settings) read from the
environment or the repository-root `.env`. It holds the datastore URLs, the
NCBI base URLs, the download directory, and the HTTP timeout. `get_settings()`
is `lru_cache`d, so settings are read once per process.

`ncbi_contact_email` is unset by default and nothing is sent to NCBI unless you
supply one yourself.

`sciterm_env` is the one runtime switch between local and prod. `local` (the
default) leaves the app exactly as it behaved before auth existed; `prod` gates
the metered routes and **refuses to start without `JWT_SECRET`**, rather than
minting tokens anyone could forge. One switch rather than a build flag, so the
same bundle and the same image serve both — see [`api/auth`](../auth).

**`main.py`** — builds the app. The `lifespan` handler creates one pooled
`httpx.AsyncClient` for the process and constructs the `PubTatorClient` and
`PmcClient` onto `app.state`, closing the client on shutdown. Sharing one pool
across both clients is deliberate: NCBI asks callers to stay under roughly
three requests per second, which is easier to honour from a single pool.

Route order matters here. The `/pb` router is included *before* `StaticFiles`
is mounted at `/`, because a root mount otherwise swallows every path. The
corpus package's `protected_router` is included before its free `router` for
the same reason at a smaller scale: registered after, `/corpus/rag_search`
would be matched by `/corpus/{paper_id}` and rejected as a bad integer.

The SPA catch-all stays open to everyone even in prod. The login gate is a
React modal, so `index.html` and `/assets` must load for an anonymous visitor;
only the JSON routes are gated. When no
built UI bundle is present, `/` returns a 503 explaining how to build it rather
than a bare 404.

## Dependencies

`fastapi`, `pydantic-settings`, `httpx`. Imports `api.auth` for the
authentication routers, `api.pb_client` and
`api.pm_client` for the clients and routers, `api.ncbi` to build the one
pooled, rate-limited HTTP client they share, and `api.cache` for the document
cache handed to the PubTator client.

## Configuration

See `.env.example` at the repository root for the recognised variables.
