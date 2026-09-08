#!/usr/bin/env bash
# Call SciTerm's admin endpoints, authenticating with your SSH key.
#
#   ./scripts/admin.sh codes [count] [base-url]     mint free access codes
#   ./scripts/admin.sh rotate-admin [base-url]      set/rotate the admin password
#   ./scripts/admin.sh rotate-admin --ask [base-url]  ...and choose it yourself
#
# There is no shared secret anywhere in this flow. The server holds only the
# public keys committed in api/authorized_keys/; this script asks for a nonce,
# signs it with the matching private key, and sends the signature. ssh-agent
# and passphrase-protected keys work, because the signing is done by
# `ssh-keygen -Y sign` rather than by reading the key ourselves.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

[ -f .env ] && set -a && . ./.env && set +a
PY=./.venv/bin/python
[ -x "$PY" ] || PY=python3
KEY="${SCITERM_ADMIN_KEY:-$HOME/.ssh/id_ed25519}"

die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null || die "curl not found"
command -v ssh-keygen >/dev/null || die "ssh-keygen not found"
[ -f "$KEY" ] || die "no private key at $KEY (set SCITERM_ADMIN_KEY)"

jqpy() { "$PY" -c "import json,sys; print(json.load(sys.stdin)$1)"; }

# Ask for a nonce, sign it, and echo "<nonce> <armored signature>".
sign_challenge() {
  local base="$1" action="$2" response nonce namespace tmp
  response="$(curl -fsS -X POST "$base/admin/challenge" \
    -H 'Content-Type: application/json' \
    -d "$("$PY" -c 'import json,sys; print(json.dumps({"action": sys.argv[1]}))' "$action")")" \
    || die "could not reach $base/admin/challenge"

  nonce="$(printf '%s' "$response" | jqpy '["nonce"]')"
  namespace="$(printf '%s' "$response" | jqpy '["namespace"]')"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  printf '%s' "$nonce" > "$tmp/nonce"
  # -n binds the signature to this namespace. Without it, a signature made for
  # any other purpose with the same key would authenticate here.
  ssh-keygen -Y sign -q -f "$KEY" -n "$namespace" "$tmp/nonce" \
    || die "signing failed"
  printf '%s\t%s' "$nonce" "$(cat "$tmp/nonce.sig")"
}

post_signed() {
  local base="$1" path="$2" action="$3" extra="$4" signed nonce signature
  signed="$(sign_challenge "$base" "$action")"
  nonce="${signed%%$'\t'*}"
  signature="${signed#*$'\t'}"
  NONCE="$nonce" SIGNATURE="$signature" EXTRA="$extra" "$PY" - <<'PY' > /tmp/.sciterm-admin-body.$$
import json, os
body = json.loads(os.environ["EXTRA"])
body["nonce"] = os.environ["NONCE"]
body["signature"] = os.environ["SIGNATURE"]
print(json.dumps(body))
PY
  curl -fsS -X POST "$base$path" -H 'Content-Type: application/json' \
    --data-binary "@/tmp/.sciterm-admin-body.$$"
  rm -f "/tmp/.sciterm-admin-body.$$"
}

cmd="${1:-}"; shift || true

case "$cmd" in
  codes)
    count="${1:-5}"; base="${2:-http://localhost:${API_PORT:-8000}}"
    # 24 urlsafe characters, ~143 bits. The server refuses anything under 16.
    codes="$("$PY" -c "import secrets; print('\n'.join(secrets.token_urlsafe(18) for _ in range($count)))")"
    extra="$("$PY" -c 'import json,sys; print(json.dumps({"codes": sys.stdin.read().split()}))' <<<"$codes")"
    post_signed "$base" "/admin/generate_codes" "generate_codes" "$extra"
    echo
    echo "--- codes (shown once) ---"
    echo "$codes"
    ;;

  rotate-admin)
    password=""
    if [ "${1:-}" = "--ask" ]; then
      shift
      printf 'New admin password (blank to generate one): ' >&2
      read -rs password; echo >&2
    fi
    base="${1:-http://localhost:${API_PORT:-8000}}"
    extra="$(PW="$password" "$PY" -c 'import json,os; print(json.dumps({"username":"admin","password":os.environ["PW"]}))')"
    out="$(mktemp)"
    post_signed "$base" "/admin/create_admin_user" "create_admin_user" "$extra" > "$out"
    "$PY" - "$out" <<'PY'
import json, sys

with open(sys.argv[1]) as handle:
    r = json.load(handle)

origin = "generated" if r["generated"] else "as supplied"
action = "rotated" if r["rotated"] else "created"
print("username: " + r["username"])
print("password: " + r["password"] + "   (" + origin + ", shown once)")
print(action + "; " + str(r["revoked_sessions"]) + " session(s) revoked")
PY
    rm -f "$out"
    ;;

  *)
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
