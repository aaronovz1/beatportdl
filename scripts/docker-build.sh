#!/bin/sh
set -eu

image=${DOCKER_IMAGE:-beatportdl-builder:local}
dockerfile=${DOCKERFILE_PATH:-Dockerfile.build}
workspace=${WORKSPACE_DIR:-$(pwd)}
command=${1:-usage}
shift || true

build_image() {
  docker build -f "$dockerfile" -t "$image" "$workspace"
}

run_image() {
  set -- docker run --rm

  if [ -t 0 ] && [ -t 1 ]; then
    set -- "$@" -it
  fi

  set -- "$@" -v "$workspace:/workspace"

  if [ -n "${MACOS_SDK_PATH:-}" ]; then
    set -- "$@" -v "${MACOS_SDK_PATH}:/opt/macos-sdk/MacOSX.sdk:ro"
  fi

  exec "$@" "$image" "$command"
}

case "$command" in
  build-image)
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
    build_image
    run_image "$@"
    ;;
esac
