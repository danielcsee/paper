#!/usr/bin/env bash
# Set the `admin` account's password.
#
#   ./scripts/set-admin-password.sh            prompt for it (does not echo)
#   ./scripts/set-admin-password.sh --show     print a hash, write nothing
#
# The password is hashed here and only the hash reaches the database, so no
# secret is ever written into a migration or a .env file.
# Until this runs, `admin` has no password hash and password login always
# fails -- which is the correct state for a fresh checkout.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SHOW_ONLY=0
[ "${1:-}" = "--show" ] && SHOW_ONLY=1

[ -f .env ] && set -a && . ./.env && set +a

PY=./.venv/bin/python
[ -x "$PY" ] || PY=python3

printf 'New password for admin: ' >&2
read -rs password
echo >&2
printf 'Repeat: ' >&2
read -rs confirm
echo >&2

[ "$password" = "$confirm" ] || { echo "error: passwords do not match" >&2; exit 1; }
[ "${#password}" -ge 12 ] || { echo "error: use at least 12 characters" >&2; exit 1; }

SCITERM_PASSWORD="$password" SCITERM_SHOW_ONLY="$SHOW_ONLY" "$PY" - <<'PY'
import os
import sys

from sqlalchemy import text

from api.auth.passwords import hash_password
from api.db import get_engine

digest = hash_password(os.environ["SCITERM_PASSWORD"])

if os.environ["SCITERM_SHOW_ONLY"] == "1":
    print(digest)
    sys.exit(0)

with get_engine().begin() as conn:
    updated = conn.execute(
        text("UPDATE users SET password_hash = :h WHERE username = 'admin'"),
        {"h": digest},
    ).rowcount

if updated != 1:
    sys.exit("error: no 'admin' row -- run `alembic upgrade head` first")

# Any session opened with the old password is now a stale key to the admin
# account, so a password change ends all of them.
with get_engine().begin() as conn:
    revoked = conn.execute(
        text(
            "UPDATE auth_sessions SET revoked_at = now() "
            "WHERE revoked_at IS NULL AND user_id = "
            "(SELECT id FROM users WHERE username = 'admin')"
        )
    ).rowcount

print(f"admin password set; {revoked} existing admin session(s) revoked")
PY
