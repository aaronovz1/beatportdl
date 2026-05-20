#!/usr/bin/env bash
set -euo pipefail

target=${1:?usage: build-release-deps.sh <target> <prefix>}
prefix=${2:?usage: build-release-deps.sh <target> <prefix>}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

third_party_root=${THIRD_PARTY_ROOT:-"$repo_root/.third_party"}
downloads_dir=${THIRD_PARTY_DOWNLOADS_DIR:-"$third_party_root/downloads"}
sources_dir=${THIRD_PARTY_SOURCES_DIR:-"$third_party_root/sources"}
builds_dir=${THIRD_PARTY_BUILDS_DIR:-"$third_party_root/builds"}

zlib_version=${ZLIB_VERSION:-1.3.2}
zlib_tarball="zlib-${zlib_version}.tar.gz"
zlib_url=${ZLIB_URL:-"https://zlib.net/${zlib_tarball}"}
zlib_sha256=${ZLIB_SHA256:-bb329a0a2cd0274d05519d61c667c062e06990d72e125ee2dfa8de64f0119d16}

taglib_version=${TAGLIB_VERSION:-2.3}
taglib_commit=${TAGLIB_COMMIT:-1b94b93762636ebe5733180c3e825be4621e4c7f}
taglib_url=${TAGLIB_URL:-https://github.com/taglib/taglib.git}

case "$target" in
  darwin-amd64|darwin-arm64)
    system_name=Darwin
    cc=${CC:-clang}
    cxx=${CXX:-clang++}
    ;;
  windows-amd64)
    system_name=Windows
    cc=${CC:-x86_64-w64-mingw32-gcc}
    cxx=${CXX:-x86_64-w64-mingw32-g++}
    ;;
  *)
    printf 'unsupported target: %s\n' "$target" >&2
    exit 1
    ;;
esac

hash_cmd=
if command -v sha256sum >/dev/null 2>&1; then
  hash_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  hash_cmd="shasum -a 256"
else
  printf 'sha256sum or shasum is required\n' >&2
  exit 1
fi

mkdir -p "$downloads_dir" "$sources_dir" "$builds_dir" "$prefix"

verify_sha256() {
  local expected=$1
  local file=$2
  local actual

  actual=$($hash_cmd "$file" | awk '{print $1}')
  if [ "$actual" != "$expected" ]; then
    printf 'sha256 mismatch for %s\nexpected %s\ngot      %s\n' "$file" "$expected" "$actual" >&2
    exit 1
  fi
}

ensure_zlib_source() {
  local tarball="$downloads_dir/$zlib_tarball"
  local source_dir="$sources_dir/zlib-$zlib_version"

  if [ ! -f "$tarball" ]; then
    curl -fsSLo "$tarball" "$zlib_url"
  fi
  verify_sha256 "$zlib_sha256" "$tarball"

  if [ ! -d "$source_dir" ]; then
    tar -xzf "$tarball" -C "$sources_dir"
  fi

  printf '%s\n' "$source_dir"
}

ensure_taglib_source() {
  local source_dir="$sources_dir/taglib-$taglib_version"

  if [ ! -d "$source_dir/.git" ]; then
    rm -rf "$source_dir"
    git clone --depth 1 --branch "v${taglib_version}" --recurse-submodules --shallow-submodules "$taglib_url" "$source_dir" >&2
  fi

  if [ "$(git -C "$source_dir" rev-parse HEAD)" != "$taglib_commit" ]; then
    printf 'unexpected TagLib commit in %s\n' "$source_dir" >&2
    exit 1
  fi

  printf '%s\n' "$source_dir"
}

build_zlib() {
  local source_dir=$1
  local build_dir="$builds_dir/zlib-$target"

  rm -rf "$build_dir"
  CC="$cc" cmake -S "$source_dir" -B "$build_dir" -G Ninja \
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
  local source_dir=$1
  local build_dir="$builds_dir/taglib-$target"

  rm -rf "$build_dir"
  CC="$cc" CXX="$cxx" cmake -S "$source_dir" -B "$build_dir" -G Ninja \
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

zlib_source=$(ensure_zlib_source)
taglib_source=$(ensure_taglib_source)

build_zlib "$zlib_source"
build_taglib "$taglib_source"
