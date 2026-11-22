#!/usr/bin/env bash
set -euo pipefail

tag=${1:?usage: release-notes.sh <tag>}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
changelog="$repo_root/CHANGELOG.md"

if [ ! -f "$changelog" ]; then
  printf 'CHANGELOG.md is missing.\n' >&2
  exit 1
fi

extract_notes() {
  awk -v version="$1" '
    /^##[[:blank:]]+/ {
      capture = ($2 == version)
      if (capture) found++
      next
    }
    capture {
      if (NF) nonblank = 1
      print
    }
    END {
      if (!found) exit 2
      if (found != 1 || !nonblank) exit 1
    }
  ' "$changelog"
}

base_tag=$(printf '%s\n' "$tag" | sed -E 's/^(v[0-9]+\.[0-9]+\.[0-9]+)-rc\.?[0-9]+(\+[0-9A-Za-z.-]+)?$/\1/')
if notes=$(extract_notes "$tag"); then
  :
else
  status=$?
  # Only a missing RC entry may borrow the exact base release's notes.
  if [ "$status" -ne 2 ] || [ "$base_tag" = "$tag" ] || ! notes=$(extract_notes "$base_tag"); then
    printf 'Expected one nonempty changelog section for %s.\n' "$tag" >&2
    exit 1
  fi
fi

printf '## %s\n\n' "$tag"
if [ "$base_tag" != "$tag" ]; then
  printf 'This is a release candidate for `%s`.\n\n' "$base_tag"
fi
printf '%s\n' "$notes"
