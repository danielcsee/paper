#!/usr/bin/env bash
# Connect to the dev Postgres container with psql.
set -euo pipefail

cd "$(dirname "$0")/.."
[ -f .env ] && set -a && source .env && set +a

exec docker exec -it litgraph-postgres psql \
  -U "${POSTGRES_USER:-litgraph}" -d "${POSTGRES_DB:-litgraph}"
