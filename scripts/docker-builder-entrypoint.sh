#!/bin/sh
set -eu

workspace=${WORKSPACE_DIR:-/workspace}
src_root=/opt/src
deps_root=/opt/deps
macos_sdk_dir=${MACOS_SDK_DIR:-/opt/macos-sdk/MacOSX.sdk}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

ensure_workspace() {
  [ -d "$workspace" ] || die "workspace not found at $workspace"
}

need_macos_sdk() {
  [ -d "$macos_sdk_dir" ] || die "macOS SDK not mounted at $macos_sdk_dir. Mount one and retry darwin builds."
}

target_triple() {
  case "$1" in
    linux-amd64) printf '%s' "x86_64-linux-gnu" ;;
    linux-arm64) printf '%s' "aarch64-linux-gnu" ;;
    windows-amd64) printf '%s' "x86_64-windows-gnu" ;;
    darwin-amd64) printf '%s' "x86_64-macos" ;;
    darwin-arm64) printf '%s' "aarch64-macos" ;;
    *) die "unsupported target: $1" ;;
  esac
}

target_system() {
  case "$1" in
    linux-*) printf '%s' "Linux" ;;
    windows-*) printf '%s' "Windows" ;;
    darwin-*) printf '%s' "Darwin" ;;
    *) die "unsupported target: $1" ;;
  esac
}

native_linux_target() {
  arch=$(go env GOARCH)
  case "$arch" in
    amd64) printf '%s' "linux-amd64" ;;
    arm64) printf '%s' "linux-arm64" ;;
    *) die "unsupported Go architecture for containerized tests: $arch" ;;
  esac
}

compiler_flags() {
  target=$1
  triple=$(target_triple "$target")
  cc="zig cc -target $triple"
  cxx="zig c++ -target $triple"

  case "$target" in
    darwin-*)
      need_macos_sdk
      sdk_flags="-isysroot $macos_sdk_dir -iwithsysroot /usr/include -iframeworkwithsysroot /System/Library/Frameworks"
      cc="$cc $sdk_flags"
      cxx="$cxx $sdk_flags"
      ;;
  esac

  printf '%s\n%s\n' "$cc" "$cxx"
}

build_zlib() {
  target=$1
  prefix=$2
  build_dir="/tmp/build-zlib-$target"
  system_name=$(target_system "$target")
  flags=$(compiler_flags "$target")
  cc=$(printf '%s' "$flags" | sed -n '1p')

  rm -rf "$build_dir"
  CC="$cc" cmake -S "$src_root/zlib" -B "$build_dir" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$prefix" \
    -DCMAKE_SYSTEM_NAME="$system_name" \
    -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DBUILD_SHARED_LIBS=OFF \
    -DZLIB_BUILD_SHARED=OFF \
    -DZLIB_BUILD_STATIC=ON \
    -DZLIB_BUILD_TESTING=OFF
  cmake --build "$build_dir" --parallel
  cmake --install "$build_dir"

  if [ "$target" = "windows-amd64" ] && [ -f "$prefix/lib/libzs.a" ] && [ ! -f "$prefix/lib/libz.a" ]; then
    cp "$prefix/lib/libzs.a" "$prefix/lib/libz.a"
  fi
}

build_taglib() {
  target=$1
  prefix=$2
  build_dir="/tmp/build-taglib-$target"
  system_name=$(target_system "$target")
  flags=$(compiler_flags "$target")
  cc=$(printf '%s' "$flags" | sed -n '1p')
  cxx=$(printf '%s' "$flags" | sed -n '2p')

  rm -rf "$build_dir"
  CC="$cc" CXX="$cxx" cmake -S "$src_root/taglib" -B "$build_dir" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$prefix" \
    -DCMAKE_SYSTEM_NAME="$system_name" \
    -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DCMAKE_PREFIX_PATH="$prefix" \
    -DZLIB_ROOT="$prefix" \
    -DHAVE_CRUN_LIB=FALSE \
    -DBUILD_SHARED_LIBS=OFF \
    -DBUILD_TESTING=OFF
  cmake --build "$build_dir" --parallel
  cmake --install "$build_dir"
}

ensure_target_deps() {
  target=$1
  prefix="$deps_root/$target"

  if [ "${FORCE_REBUILD_DEPS:-0}" = "1" ]; then
    rm -rf "$prefix"
  fi

  if [ ! -f "$prefix/include/taglib/tag_c.h" ] || {
    [ ! -f "$prefix/lib/libtag_c.a" ] && [ ! -f "$prefix/lib/libtag_c.dll.a" ]
  }; then
    mkdir -p "$prefix"
    build_zlib "$target" "$prefix"
    build_taglib "$target" "$prefix"
  fi
}

run_make_target() {
  target=$1
  prefix="$deps_root/$target"
  ensure_target_deps "$target"

  case "$target" in
    linux-amd64)
      env LINUX_AMD64_LIB_PATH="-L$prefix/lib -I$prefix/include" make -C "$workspace" "$target"
      ;;
    linux-arm64)
      env LINUX_ARM64_LIB_PATH="-L$prefix/lib -I$prefix/include" make -C "$workspace" "$target"
      ;;
    windows-amd64)
      env WINDOWS_AMD64_LIB_PATH="-L$prefix/lib -I$prefix/include" make -C "$workspace" "$target"
      ;;
    darwin-amd64)
      need_macos_sdk
      env \
        MACOS_SDK_PATH="$macos_sdk_dir" \
        MACOS_AMD64_LIB_PATH="-L$prefix/lib -I$prefix/include" \
        make -C "$workspace" "$target"
      ;;
    darwin-arm64)
      need_macos_sdk
      env \
        MACOS_SDK_PATH="$macos_sdk_dir" \
        MACOS_ARM64_LIB_PATH="-L$prefix/lib -I$prefix/include" \
        make -C "$workspace" "$target"
      ;;
    *)
      die "unsupported build target: $target"
      ;;
  esac
}

pure_test() {
  cd "$workspace"
  go test ./config ./internal/beatport ./internal/validator
}

full_test() {
  target=$(native_linux_target)
  prefix="$deps_root/$target"
  flags=$(compiler_flags "$target")
  cc=$(printf '%s' "$flags" | sed -n '1p')
  cxx=$(printf '%s' "$flags" | sed -n '2p')
  ensure_target_deps "$target"
  cd "$workspace"
  env \
    CC="$cc -I$prefix/include -DTAGLIB_STATIC -Wall" \
    CXX="$cxx -I$prefix/include -DTAGLIB_STATIC -Wall" \
    CGO_CPPFLAGS="-I$prefix/include" \
    CGO_CFLAGS="-I$prefix/include -DTAGLIB_STATIC -Wall" \
    CGO_CXXFLAGS="-I$prefix/include -DTAGLIB_STATIC -Wall" \
    CGO_LDFLAGS="-L$prefix/lib" \
    go test -ldflags "-linkmode external" ./...
}

release_core() {
  run_make_target linux-amd64
  run_make_target linux-arm64
}

release_all() {
  release_core
  run_make_target windows-amd64
  run_make_target darwin-amd64
  run_make_target darwin-arm64
}

usage() {
  cat <<'EOF'
Usage: beatportdl-builder <command>

Commands:
  pure-test       Run Go tests that do not require TagLib.
  test            Run the full Go test suite with container-built TagLib.
  linux-amd64     Build the Linux AMD64 binary.
  linux-arm64     Build the Linux ARM64 binary.
  windows-amd64   Build the Windows AMD64 binary.
  darwin-amd64    Build the macOS AMD64 binary. Requires a mounted macOS SDK.
  darwin-arm64    Build the macOS ARM64 binary. Requires a mounted macOS SDK.
  release-core    Build the supported Linux AMD64 and Linux ARM64 binaries.
  release-all     Build Linux, Windows, and macOS release binaries. Requires a mounted macOS SDK.
  shell           Open a shell in the builder image.
EOF
}

ensure_workspace

command=${1:-usage}
case "$command" in
  pure-test) pure_test ;;
  test) full_test ;;
  linux-amd64|linux-arm64|windows-amd64|darwin-amd64|darwin-arm64) run_make_target "$command" ;;
  release-core) release_core ;;
  release-all) release_all ;;
  shell) exec /bin/sh ;;
  usage|-h|--help|help) usage ;;
  *) die "unknown command: $command" ;;
esac
