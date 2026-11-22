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

prepare_staging() {
  staging=$(mktemp -d "${TMPDIR:-/tmp}/beatportdl-build.XXXXXX")
  python3 - "$workspace" "$staging" "$dockerfile" "$1" <<'PY'
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys

root, staging, dockerfile = map(Path, sys.argv[1:4])
root = Path.cwd() / root
directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


def checked_relative(path):
    if path.is_absolute() or not path.parts or '..' in path.parts:
        raise SystemExit('unsafe source path: ' + str(path))
    return path


def open_directory(base, parts):
    directory = os.dup(base)
    try:
        for part in parts:
            child = os.open(part, directory_flags, dir_fd=directory)
            os.close(directory)
            directory = child
        return directory
    except OSError:
        os.close(directory)
        raise


def copy_regular(base, path, target, *, allow_missing=False, preserve_mode=False):
    # Walk from the pinned workspace without following parent or leaf links.
    # Open nonblocking so a non-regular leaf cannot hang before fstat.
    try:
        directory = open_directory(base, path.parts[:-1])
        try:
            fd = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        finally:
            os.close(directory)
    except OSError as error:
        if allow_missing and isinstance(error, FileNotFoundError):
            return  # Deleted tracked paths stay deleted in the snapshot.
        raise SystemExit('unsafe source path (symlink or inaccessible): %s: %s' % (path, error))
    with os.fdopen(fd, 'rb') as source:
        source_mode = os.fstat(source.fileno()).st_mode
        if not stat.S_ISREG(source_mode):
            raise SystemExit('non-regular source file: ' + str(path))
        target.parent.mkdir(parents=True, exist_ok=True)
        mode = source_mode & 0o777 if preserve_mode else 0o666
        with open(target, 'wb', opener=lambda name, flags: os.open(name, flags, mode)) as output:
            shutil.copyfileobj(source, output)


def git_files(*options):
    return subprocess.check_output([
        'git', '-C', str(root), '--literal-pathspecs', 'ls-files', '-z', *options
    ]).split(b'\0')


def prepare_context(base):
    # Never send the repository (or its credentials) to the Docker daemon.
    inputs = [Path('Dockerfile.build'), *(Path('scripts') / script for script in
              ('docker-builder-entrypoint.sh', 'zig-ar.sh', 'zig-ranlib.sh'))]
    try:
        selected = dockerfile.relative_to(root) if dockerfile.is_absolute() else dockerfile
    except ValueError:
        raise SystemExit('Dockerfile must be inside the workspace: ' + str(dockerfile))
    sources = [checked_relative(selected), *inputs[1:]]
    tracked = set()
    for entry in filter(None, git_files('--stage', '--', *map(str, sources))):
        metadata, path = entry.split(b'\t', 1)
        mode, _, stage = metadata.split()
        if stage == b'0' and mode in {b'100644', b'100755'}:
            tracked.add(path)
    for source, target in zip(sources, inputs):
        if os.fsencode(source) not in tracked:
            raise SystemExit('Docker build input is not a tracked regular file: ' + str(source))
        copy_regular(base, source, staging / 'context' / target, preserve_mode=True)


def prepare_source(base):
    destination = staging / 'source'
    destination.mkdir()
    paths = git_files('--cached', '--others', '--exclude-standard')
    source_extensions = {'.go', '.c', '.h', '.cc', '.cpp', '.hpp', '.s', '.S', '.syso'}
    for raw in sorted(set(paths) - {b''}):
        path = checked_relative(Path(os.fsdecode(raw)))
        parts = path.parts
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
        copy_regular(base, path, destination / path, allow_missing=True)


if '..' in root.parts:
    raise SystemExit('unsafe workspace path: ' + str(root))
try:
    anchor = os.open(root.anchor, directory_flags)
    try:
        base = open_directory(anchor, root.parts[1:])
    finally:
        os.close(anchor)
except OSError as error:
    raise SystemExit('unsafe workspace path (symlink or inaccessible): %s: %s' % (root, error))
try:
    prepare_context(base)
    if sys.argv[4] == 'all':
        prepare_source(base)
finally:
    os.close(base)
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
    prepare_staging context
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
    prepare_staging all
    build_image
    run_image "$@"
    ;;
esac
