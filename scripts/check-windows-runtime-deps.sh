#!/usr/bin/env bash
set -euo pipefail

exe=${1:?usage: check-windows-runtime-deps.sh <windows-exe>}

if command -v x86_64-w64-mingw32-objdump >/dev/null 2>&1; then
  objdump_cmd=x86_64-w64-mingw32-objdump
elif command -v objdump >/dev/null 2>&1; then
  objdump_cmd=objdump
else
  printf 'objdump is required to inspect Windows runtime dependencies\n' >&2
  exit 1
fi

if [ ! -f "$exe" ]; then
  printf 'Windows executable not found: %s\n' "$exe" >&2
  exit 1
fi

imports=$("$objdump_cmd" -p "$exe" | awk '/DLL Name:/ {print $3}')

printf 'Windows DLL imports for %s:\n' "$exe"
printf '%s\n' "$imports"

for forbidden in libwinpthread-1.dll libstdc++-6.dll libgcc_s_seh-1.dll; do
  if printf '%s\n' "$imports" | grep -Fxi "$forbidden" >/dev/null; then
    printf 'unexpected dynamic MinGW runtime dependency: %s\n' "$forbidden" >&2
    exit 1
  fi
done
