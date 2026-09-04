#!/usr/bin/env bash
# Launch the whole local stack: Postgres + Neo4j in Docker, the FastAPI server
# and the React dev server on the host.
#
#   ./scripts/dev.sh           hot-reloading dev servers (UI on :5173)
#   ./scripts/dev.sh --prod    build the bundle and serve it from FastAPI (:8000)
#   ./scripts/dev.sh --no-db   skip Docker; app processes only
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PROD=0
START_DB=1
for arg in "$@"; do
  case "$arg" in
    --prod)   PROD=1 ;;
    --no-db)  START_DB=0 ;;
    -h|--help) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ -f .env ] || { log "creating .env from .env.example"; cp .env.example .env; }
set -a; . ./.env; set +a
API_PORT="${API_PORT:-8000}"
UI_PORT="${UI_PORT:-5173}"

# ---------- datastores ----------
if [ "$START_DB" = 1 ]; then
  command -v docker >/dev/null || die "docker not found (or run with --no-db)"
  log "starting postgres + neo4j"
  docker compose up -d postgres neo4j
  log "waiting for healthchecks (neo4j downloads the GDS plugin on first run)"
  for _ in $(seq 1 90); do
    unhealthy=$(docker compose ps --format '{{.Service}} {{.Health}}' \
                 | awk '$2 != "healthy" {print $1}' | tr '\n' ' ')
    [ -z "$unhealthy" ] && break
    sleep 2
  done
  [ -n "${unhealthy:-}" ] && log "still starting: ${unhealthy}— continuing anyway"
fi

# ---------- python env ----------
command -v python3 >/dev/null || die "python3 not found"
if [ ! -d .venv ]; then
  log "creating .venv ($(python3 --version))"
  python3 -m venv .venv
fi
log "installing python dependencies"
./.venv/bin/python -m pip install --quiet --upgrade pip
./.venv/bin/python -m pip install --quiet -r api/requirements.txt

# ---------- schema ----------
if [ "$START_DB" = 1 ]; then
  log "applying database migrations"
  ./.venv/bin/alembic upgrade head
fi

# ---------- node deps ----------
command -v npm >/dev/null || die "npm not found"
if [ ! -d ui/node_modules ]; then
  log "installing ui dependencies"
  npm --prefix ui install
fi

PIDS=()
cleanup() {
  trap - INT TERM EXIT
  [ ${#PIDS[@]} -gt 0 ] && kill "${PIDS[@]}" 2>/dev/null || true
  wait 2>/dev/null || true
  echo
  log "stopped (datastores still running — 'docker compose down' to stop them)"
}
trap cleanup INT TERM EXIT

if [ "$PROD" = 1 ]; then
  log "building ui bundle"
  npm --prefix ui run build
  log "FastAPI serving the bundle on http://localhost:${API_PORT}"
  ./.venv/bin/uvicorn api.app.main:app --host 0.0.0.0 --port "$API_PORT" &
  PIDS+=($!)
else
  log "api  → http://localhost:${API_PORT}"
  ./.venv/bin/uvicorn api.app.main:app --host 0.0.0.0 --port "$API_PORT" --reload &
  PIDS+=($!)
  log "ui   → http://localhost:${UI_PORT}"
  API_PORT="$API_PORT" UI_PORT="$UI_PORT" npm --prefix ui run dev &
  PIDS+=($!)
fi

log "neo4j browser → http://localhost:${NEO4J_HTTP_PORT:-7474}"
log "ctrl-c to stop"
wait
