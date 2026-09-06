#!/usr/bin/env bash
# Build the Neo4j knowledge graph from what is already in Postgres.
#
#   ./scripts/build-graph.sh                 apply schema, then load
#   ./scripts/build-graph.sh --schema-only   constraints and indexes only
#   ./scripts/build-graph.sh --reset         wipe nodes and edges, then reload
#
# Idempotent: re-run it after importing more papers.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

[ -f .env ] && { set -a; . ./.env; set +a; }
[ -x .venv/bin/python ] || { echo "error: .venv missing — run ./scripts/dev.sh first" >&2; exit 1; }

exec ./.venv/bin/python -m api.graph.build "$@"
