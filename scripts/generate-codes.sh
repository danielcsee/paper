#!/usr/bin/env bash
# Mint free access codes and post them to a running SciTerm.
#
#   ./scripts/generate-codes.sh 5                       5 codes, local API
#   ./scripts/generate-codes.sh 5 https://sciterm.example.com
#
# Prompts for the admin password, exchanges it for an access token, then calls
# POST /admin/generate_codes. The codes are printed once, here -- the server
# stores them but this is the only place they are shown alongside their purpose.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

COUNT="${1:-5}"
[ -f .env ] && set -a && . ./.env && set +a
BASE="${2:-http://localhost:${API_PORT:-8000}}"

command -v curl >/dev/null || { echo "error: curl not found" >&2; exit 1; }
PY=./.venv/bin/python
[ -x "$PY" ] || PY=python3

printf 'admin password for %s: ' "$BASE" >&2
read -rs password
echo >&2

token="$(curl -fsS -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$("$PY" -c 'import json,sys; print(json.dumps({"username":"admin","password":sys.argv[1]}))' "$password")" \
  | "$PY" -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"

# 24 urlsafe characters, ~143 bits. The server refuses anything under 16.
codes="$("$PY" -c "import secrets; print('\n'.join(secrets.token_urlsafe(18) for _ in range($COUNT)))")"

payload="$("$PY" -c 'import json,sys; print(json.dumps({"token": sys.argv[1], "codes": sys.stdin.read().split()}))' "$token" <<<"$codes")"
curl -fsS -X POST "$BASE/admin/generate_codes" \
  -H 'Content-Type: application/json' -d "$payload"
echo

echo "--- codes (shown once) ---"
echo "$codes"
