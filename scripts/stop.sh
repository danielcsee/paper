#!/usr/bin/env bash
# Stop everything this project starts: host servers and its Docker containers.
#
#   ./scripts/stop.sh              stop app processes and the datastores
#   ./scripts/stop.sh --apps-only  leave Postgres/Neo4j/Redis running
#   ./scripts/stop.sh --quiet      say less
#   ./scripts/stop.sh --dry-run    list what would be stopped, kill nothing
#
# Finds processes by what they are, not by who started them, so a server
# launched by hand outside dev.sh is stopped too. That matters: two uvicorns can
# hold :8000 at once -- one bound to 127.0.0.1, one to 0.0.0.0 -- and the kernel
# prefers the specific bind, so the stale one keeps serving localhost silently.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

APPS_ONLY=0
QUIET=0
DRY=0
for arg in "$@"; do
  case "$arg" in
    --apps-only) APPS_ONLY=1 ;;
    --quiet|-q)  QUIET=1 ;;
    --dry-run)   DRY=1 ;;
    -h|--help)   sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

log()  { [ "$QUIET" = 1 ] || printf '\033[1;34m==>\033[0m %s\n' "$*"; }
run()  { if [ "$DRY" = 1 ]; then printf '   would run: %s\n' "$*"; else "$@"; fi; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*" >&2; }

# What each process calls itself. Matched against the full command line.
PATTERNS='uvicorn api.app.main
celery -A api.ingestion.celery_app
node_modules/.bin/vite'

# A PID belongs to us if its command line names this checkout, or its working
# directory is this checkout. The second test is what catches `./.venv/bin/...`
# invocations, whose command lines are relative and identical between clones.
# Readable one-liner for a PID: drop the interpreter path, keep the command.
describe() {
  ps -o command= -p "$1" 2>/dev/null \
    | sed -e 's|[^ ]*/\.venv/bin/|.venv/bin/|' \
          -e "s|$ROOT/|./|g" \
          -e 's|^[^ ]*/Python ||' \
    | cut -c1-72
}

belongs_to_us() {
  local pid="$1" cmd cwd
  cmd="$(ps -o command= -p "$pid" 2>/dev/null || true)"
  case "$cmd" in *"$ROOT"*) return 0 ;; esac
  cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
  [ "$cwd" = "$ROOT" ]
}

find_pids() {
  local pattern pid
  printf '%s\n' "$PATTERNS" | while IFS= read -r pattern; do
    [ -n "$pattern" ] || continue
    for pid in $(pgrep -f "$pattern" 2>/dev/null || true); do
      [ "$pid" = "$$" ] && continue
      belongs_to_us "$pid" && echo "$pid"
    done
  done | sort -un
}

pids="$(find_pids)"

if [ -z "$pids" ]; then
  log "no app processes running"
else
  for pid in $pids; do
    log "stopping $pid  $(describe "$pid")"
  done
  # SIGTERM first so uvicorn closes its socket and Celery finishes its task.
  if [ "$DRY" = 1 ]; then
    run kill $pids
    pids=""
  else
    kill $pids 2>/dev/null || true
  fi
  for _ in $(seq 1 20); do
    still="$(for pid in $pids; do kill -0 "$pid" 2>/dev/null && echo "$pid"; done)"
    [ -z "$still" ] && break
    sleep 0.5
  done
  if [ -n "${still:-}" ]; then
    warn "still alive after 10s, sending SIGKILL: $(echo $still | tr '\n' ' ')"
    kill -9 $still 2>/dev/null || true
  fi
fi

if [ "$APPS_ONLY" = 1 ]; then
  log "datastores left running (--apps-only)"
else
  if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
    log "stopping datastores"
    run docker compose down --remove-orphans
  else
    warn "docker unavailable; datastores not touched"
  fi
fi

# The symptom that started all this: something still answering on the API port.
# A second pass, because uvicorn's --reload child is spawned with a command line
# that names neither uvicorn nor the app, so the patterns above never match it --
# it normally dies with its parent, but a stale one holding the port is exactly
# the failure this script exists to prevent.
API_PORT="${API_PORT:-8000}"
if [ "$DRY" = 1 ]; then
  log "port :$API_PORT not checked (--dry-run)"
  log "stopped"
  exit 0
fi

leftover="$(lsof -nP -iTCP:"$API_PORT" -sTCP:LISTEN -t 2>/dev/null | sort -u || true)"
mine=""
for pid in $leftover; do
  belongs_to_us "$pid" && mine="$mine $pid"
done
if [ -n "$mine" ]; then
  log "still holding :$API_PORT, ours -> killing:$mine"
  kill -9 $mine 2>/dev/null || true
  sleep 1
  leftover="$(lsof -nP -iTCP:"$API_PORT" -sTCP:LISTEN -t 2>/dev/null | sort -u || true)"
fi
if [ -n "$leftover" ]; then
  warn "still listening on :$API_PORT: $(echo $leftover | tr '\n' ' ') -- not this checkout"
  warn "inspect with: lsof -nP -iTCP:$API_PORT -sTCP:LISTEN"
else
  log "port $API_PORT is clear"
fi

log "stopped"
