#!/usr/bin/env bash
# Remove this project's leftover Docker containers.
#
#   ./scripts/cleanup.sh          remove THIS project's containers and orphans
#   ./scripts/cleanup.sh --full   remove EVERY container on this host, drop this
#                                 project's volumes and images, and delete .venv,
#                                 ui/node_modules and ui/dist for a clean rebuild
#   ./scripts/cleanup.sh --dry-run  print what would happen, change nothing
#   ./scripts/cleanup.sh --yes      skip the confirmation prompt (for scripts)
#
# --full is not scoped to this project: it removes containers belonging to every
# other Compose project on this machine too, because that is what "all" means.
# It prints them by name and asks first. Volumes and images stay scoped to this
# project -- "rebuild the project" should not mean "delete another project's
# database".
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
PROJECT="$(basename "$ROOT")"

FULL=0
DRY=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --full)    FULL=1 ;;
    --dry-run) DRY=1 ;;
    --yes|-y)  ASSUME_YES=1 ;;
    -h|--help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
run()  { if [ "$DRY" = 1 ]; then printf '   would run: %s\n' "$*"; else "$@"; fi; }

command -v docker >/dev/null || die "docker not found"
docker info >/dev/null 2>&1 || die "docker is not running"

ours()   { docker ps -aq --filter "label=com.docker.compose.project=$PROJECT"; }
others() { docker ps -a --format '{{.ID}} {{.Names}} {{.Label "com.docker.compose.project"}}' \
             | awk -v p="$PROJECT" '$3 != p'; }

# ---------------------------------------------------------------- scoped clean
if [ "$FULL" = 0 ]; then
  log "removing $PROJECT containers and any orphans"
  run docker compose down --remove-orphans
  stragglers="$(ours || true)"
  if [ -n "$stragglers" ]; then
    log "removing containers compose left behind"
    run docker rm -f $stragglers
  fi
  log "removing this project's unused networks"
  run docker network prune -f --filter "label=com.docker.compose.project=$PROJECT"
  log "done. Volumes kept -- your imported papers are safe."
  exit 0
fi

# ------------------------------------------------------------------ full clean
all_containers="$(docker ps -aq || true)"
foreign="$(others || true)"
volumes="$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT" || true)"
# Volumes predating the label, matched by Compose's <project>_<name> convention.
volumes="$(printf '%s\n%s\n' "$volumes" "$(docker volume ls -q | grep "^${PROJECT}_" || true)" \
           | grep -v '^$' | sort -u || true)"
images="$(docker images -q "${PROJECT}"'*' 2>/dev/null || true)"

echo
warn "--full is destructive. This will:"
echo "  * remove ALL $(echo "$all_containers" | grep -c . || echo 0) containers on this Docker host"
if [ -n "$foreign" ]; then
  echo "    including these, which are NOT part of $PROJECT:"
  echo "$foreign" | awk '{printf "      - %s  (project: %s)\n", $2, ($3=="" ? "<none>" : $3)}'
fi
echo "  * delete these volumes, and every paper imported into them:"
if [ -n "$volumes" ]; then echo "$volumes" | sed 's/^/      - /'; else echo "      (none)"; fi
[ -n "$images" ] && echo "  * remove $(echo "$images" | grep -c .) image(s) built for $PROJECT"
echo "  * delete .venv/, ui/node_modules/ and ui/dist/"
echo
warn "it does NOT touch other projects' volumes or images."
echo

if [ "$DRY" = 0 ] && [ "$ASSUME_YES" = 0 ]; then
  printf 'Type the project name (%s) to continue: ' "$PROJECT"
  read -r reply
  [ "$reply" = "$PROJECT" ] || die "aborted"
fi

log "stopping app processes first"
run "$ROOT/scripts/stop.sh" --apps-only --quiet

if [ -n "$all_containers" ]; then
  log "removing all containers"
  run docker rm -f $all_containers
else
  log "no containers to remove"
fi

if [ -n "$volumes" ]; then
  log "removing $PROJECT volumes"
  run docker volume rm -f $volumes
fi

if [ -n "$images" ]; then
  log "removing $PROJECT images"
  run docker rmi -f $images
fi

log "removing local build artifacts"
for path in .venv ui/node_modules ui/dist; do
  [ -e "$ROOT/$path" ] || continue
  log "  $path"
  run rm -rf "${ROOT:?}/$path"
done

echo
log "clean. Rebuild with: ./scripts/dev.sh"
log "first run is slow: it recreates .venv, reinstalls npm packages, re-pulls"
log "images, and Neo4j downloads the GDS plugin again."
