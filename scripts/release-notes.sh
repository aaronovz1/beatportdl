#!/usr/bin/env bash
set -euo pipefail

tag=${1:?usage: release-notes.sh <tag>}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
changelog="$repo_root/CHANGELOG.md"

base_tag=${tag%%-rc*}
is_prerelease=0

if [ "$base_tag" != "$tag" ]; then
  is_prerelease=1
fi

if [ ! -f "$changelog" ]; then
  printf 'Release %s\n\nNo changelog entry found.\n' "$tag"
  exit 0
fi

notes=$(
  awk -v heading="## ${base_tag}" '
    index($0, heading) == 1 { capture=1; next }
    capture && /^## / { exit }
    capture { print }
  ' "$changelog"
)

printf '## %s\n\n' "$tag"

if [ "$is_prerelease" -eq 1 ]; then
  printf 'This is a release candidate for `%s`.\n\n' "$base_tag"
fi

if [ -n "$notes" ]; then
  printf '%s\n' "$notes"
else
  printf 'Release notes for this tag have not been added to [CHANGELOG.md](CHANGELOG.md) yet.\n'
fi
