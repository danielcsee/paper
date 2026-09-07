#!/usr/bin/env bash
# Delete local branches already merged into dev.
set -euo pipefail

cd "$(dirname "$0")/.."

BASE_BRANCH="${1:-dev}"
git rev-parse --verify "$BASE_BRANCH" >/dev/null 2>&1 || {
  echo "error: no such branch: $BASE_BRANCH" >&2
  exit 1
}

current="$(git rev-parse --abbrev-ref HEAD)"

# mapfile needs bash 4+; macOS ships bash 3.2, so read into the array by hand.
branches=()
while IFS= read -r branch; do
  branches+=("$branch")
done < <(
  git branch --merged "$BASE_BRANCH" --format='%(refname:short)' \
    | grep -vFx -e "$BASE_BRANCH" -e "$current"
)
unmerged=()
while IFS= read -r branch; do
  unmerged+=("$branch")
done < <(
  git branch --no-merged "$BASE_BRANCH" --format='%(refname:short)' \
    | grep -vFx -e "$BASE_BRANCH" -e "$current"
)

if [ "${#unmerged[@]}" -gt 0 ]; then
  echo "Branches NOT merged into '$BASE_BRANCH' (left alone):"
  printf '  %s\n' "${unmerged[@]}"
fi

if [ "${#branches[@]}" -eq 0 ]; then
  echo "No merged branches to delete."
  exit 0
fi

echo "Branches merged into '$BASE_BRANCH' that will be deleted:"
printf '  %s\n' "${branches[@]}"

read -r -p "Delete these ${#branches[@]} branch(es)? [y/N] " reply
case "$reply" in
  [yY]|[yY][eE][sS]) ;;
  *) echo "Aborted."; exit 0 ;;
esac

git branch -d "${branches[@]}"
