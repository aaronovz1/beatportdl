#!/bin/sh
set -eu

image=${DOCKER_IMAGE:-beatportdl-builder:local}
dockerfile=${DOCKERFILE_PATH:-Dockerfile.build}
workspace=${WORKSPACE_DIR:-$(pwd)}
command=${1:-usage}
shift || true
staging=

cleanup() {
  if [ -n "$staging" ]; then rm -rf "$staging"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

prepare_context() {
  staging=$(mktemp -d "${TMPDIR:-/tmp}/beatportdl-build.XXXXXX")
  mkdir -p "$staging/context/scripts"
  # Never send the repository (or its credentials) to the Docker daemon.
  case "$dockerfile" in
    /*) cp "$dockerfile" "$staging/context/Dockerfile.build" ;;
    *) cp "$workspace/$dockerfile" "$staging/context/Dockerfile.build" ;;
  esac
  for script in docker-builder-entrypoint.sh zig-ar.sh zig-ranlib.sh; do
    cp "$workspace/scripts/$script" "$staging/context/scripts/$script"
  done
}

prepare_source() {
  python3 - "$workspace" "$staging/source" <<'PY'
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys

root, destination = map(Path, sys.argv[1:])
destination.mkdir()
paths = subprocess.check_output([
    'git', '-C', str(root), 'ls-files', '-z', '--cached', '--others', '--exclude-standard'
]).split(b'\0')
source_extensions = {'.go', '.c', '.h', '.cc', '.cpp', '.hpp', '.s', '.S', '.syso'}
for raw in sorted(set(paths)):
    if not raw:
        continue
    path = Path(os.fsdecode(raw))
    parts = path.parts
    if path.is_absolute() or '..' in parts:
        raise SystemExit('unsafe source path: ' + str(path))
    lower = [part.lower() for part in parts]
    if any(part.startswith('.') or part in {'node_modules', 'downloads', 'temp', 'credentials.json',
                                           'credentials.yml', 'credentials.yaml', 'beatportdl-credentials.json'}
           or part.endswith(('.pem', '.key')) or '.log' in part for part in lower):
        continue
    if any(part in {'config.yml', 'config.yaml', 'config.json'} or part.startswith('beatportdl-config')
           for part in lower):
        continue
    if str(path) not in {'Makefile', 'go.mod', 'go.sum', 'vendor/modules.txt'}:
        if parts[0] not in {'cmd', 'config', 'internal', 'vendor'}:
            continue
        if path.suffix not in source_extensions and 'testdata' not in parts:
            continue
    # Walk using no-follow descriptors, including parent directories. A symlink
    # must not smuggle a host secret into an otherwise allowed source filename.
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
            os.close(directory)
            directory = child
        try:
            fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
        except FileNotFoundError:
            continue  # A tracked file deleted in the working tree stays deleted.
        with os.fdopen(fd, 'rb') as source:
            if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                raise SystemExit('non-regular source file: ' + str(path))
            target = destination / path
            target.parent.mkdir(parents=True, exist_ok=True)
            with target.open('wb') as output:
                shutil.copyfileobj(source, output)
    except FileNotFoundError:
        continue  # Deleted tracked directories must not reappear in the snapshot.
    except OSError as error:
        raise SystemExit('unsafe source path (symlink or inaccessible): %s: %s' % (path, error))
    finally:
        os.close(directory)
PY
}

build_image() {
  docker build -f "$staging/context/Dockerfile.build" -t "$image" "$staging/context"
}

run_image() {
  set -- docker run --rm

  if [ -t 0 ] && [ -t 1 ]; then
    set -- "$@" -it
  fi

  output=${DOCKER_OUTPUT_DIR:-$workspace/bin}
  mkdir -p "$output"
  output=$(cd "$output" && pwd)
  case "$staging$output${MACOS_SDK_PATH:-}" in
    *,*) printf 'Docker mount paths must not contain commas\n' >&2; exit 1 ;;
  esac
  set -- "$@" --network=none --cap-drop=ALL --security-opt=no-new-privileges \
    --read-only --user "$(id -u):$(id -g)" \
    --tmpfs /tmp:rw,exec,nosuid,nodev,mode=1777 \
    --mount "type=bind,src=$staging/source,dst=/workspace,readonly" \
    --mount "type=bind,src=$output,dst=/output"

  if [ -n "${MACOS_SDK_PATH:-}" ]; then
    set -- "$@" --mount "type=bind,src=${MACOS_SDK_PATH},dst=/opt/macos-sdk/MacOSX.sdk,readonly"
  fi

  "$@" "$image" "$command"
}

case "$command" in
  build-image)
    prepare_context
    build_image
    ;;
  usage|-h|--help|help)
    cat <<'EOF'
Usage: scripts/docker-build.sh <command>

Commands:
  build-image     Build the pinned builder image.
  pure-test       Run tests that do not require TagLib.
  test            Run the full test suite in the container.
  linux-amd64     Build the Linux AMD64 binary.
  linux-arm64     Build the Linux ARM64 binary.
  windows-amd64   Build the Windows AMD64 binary.
  darwin-amd64    Build the macOS AMD64 binary. Requires MACOS_SDK_PATH.
  darwin-arm64    Build the macOS ARM64 binary. Requires MACOS_SDK_PATH.
  release-core    Build Linux AMD64 and Linux ARM64.
  release-all     Build all release targets. Requires MACOS_SDK_PATH.
  shell           Open a shell inside the builder image.
EOF
    ;;
  *)
    prepare_context
    prepare_source
    build_image
    run_image "$@"
    ;;
esac
