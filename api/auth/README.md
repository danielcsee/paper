# api/auth

Access control: two accounts, their sessions, and the time-limited free access
codes that let a visitor open one.

There is no user-owned data anywhere in SciTerm — papers, chunks, entities and
the graph are a single global pool. A user here is a key to the expensive
doors, not an owner of anything.

## The invariant

`anonfree` can never hold more live tokens than there are activated access
codes. That is enforced by the schema, not by counting:

* every anonfree session names the code that minted it;
* a partial unique index allows one un-revoked session per code;
* composite foreign keys mirror `users.is_anonymous` and
  `free_access_codes.activated` onto the session row, so row-local CHECKs can
  see them — Postgres cannot put a subquery in a CHECK.

The consequence is one *seat* per code: redeeming a code on a second device
revokes the first session rather than adding one.

## Files

**`models.py`** — `users`, `free_access_codes`, `auth_sessions`, and the
constraints above. Read this first; the comments there are the design.

**`tokens.py`** — a short-lived access-token JWT (verified from its signature,
no per-request database hit) and an opaque refresh token stored only as a
sha256 digest and rotated on every use.

**`service.py`** — redeem, login, refresh, logout, generate. Redemption
activates the code, moves the seat and opens the session in one transaction.
A code's `activation_date` is stamped once and never restamped: restamping
would put the 48-hour deadline permanently out of reach.

**`dependencies.py`** — `require_user` / `require_admin`. Attached at *router*
level in `pb_client`, `pm_client`, `ingestion` and the corpus package's
`protected_router`, so a route added to one of those is gated the day it is
written.

**`routes.py`** — `/auth/*` and `/admin/generate_codes`. The admin endpoint
takes an ordinary admin access token, in the body or as a bearer header; there
is no second shared secret to leak.

**`passwords.py`** — PBKDF2-HMAC-SHA256. Not scrypt: `hashlib.scrypt` is absent
unless CPython was linked against an OpenSSL that offers it, and it is missing
from the Python in `.venv`.

## What is gated

Reading the corpus is free, so the whole UI loads with real papers for an
anonymous visitor. Gated: `/import`, `/pb`, `/pm`, `/corpus/rag_search` and
`/corpus/{id}/references` — everything that spends the NCBI budget or real
compute.

## Configuration

**`config.py`** — `AuthSettings`: the signing key, token lifetimes and cookie
policy, kept out of `api.app.config` on purpose. The Celery worker imports this
package transitively (the client packages export their routers), so the class
is constructed lazily through `get_auth_settings()`: importing the module must
never demand a secret, only calling it does. The upshot is that the worker runs
with no `JWT_SECRET` at all, and the prod fail-closed check binds the API
alone.

`SCITERM_ENV=local` (the default) switches all of this off and yields a local
admin, so development is exactly as it was before auth existed. `prod` requires
`JWT_SECRET` and refuses to start without one. See `.env.example`.

Set the admin password with `scripts/set-admin-password.sh`; issue codes with
`scripts/generate-codes.sh`.

## Dependencies

`fastapi`, `PyJWT`, `SQLAlchemy`, and `api.db` for the session factory.
