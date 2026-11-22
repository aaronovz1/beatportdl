#!/usr/bin/env bash
set -euo pipefail

target=${1:?usage: build-release-target.sh <target> <deps-prefix>}
deps_prefix=${2:?usage: build-release-target.sh <target> <deps-prefix>}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

build_dir=${BUILD_DIR:-"$repo_root/bin"}
build_src=${BUILD_SRC:-./cmd/beatportdl}

mkdir -p "$build_dir"

# Go parses CGO flags with its own argument splitter. Quote the whole argument
# rather than just the path after -I, -L, or -F.
quote_cgo_arg() {
  case "$1" in
    *"'"*'"'*|*'"'*"'"*)
      printf 'Cannot encode a native build argument containing both quote styles.\n' >&2
      return 1
      ;;
    *"'"*) printf '"%s"' "$1" ;;
    *) printf "'%s'" "$1" ;;
  esac
}

goos=
goarch=
output_name=
cc=
cxx=
cgo_cppflags=$(quote_cgo_arg "-I${deps_prefix}/include")
cgo_cflags=$cgo_cppflags
cgo_cxxflags=$cgo_cppflags
cgo_ldflags=$(quote_cgo_arg "-L${deps_prefix}/lib")
extldflags="-lstdc++"
extld=
buildmode_args=(-buildmode pie)

case "$target" in
  darwin-arm64)
    goos=darwin
    goarch=arm64
    output_name=beatportdl-darwin-arm64
    cc=${CC:-clang}
    cxx=${CXX:-clang++}
    extld="$cc"
    sdk_path=${MACOS_SDK_PATH:-$(xcrun --sdk macosx --show-sdk-path)}
    ;;
  darwin-amd64)
    goos=darwin
    goarch=amd64
    output_name=beatportdl-darwin-amd64
    cc=${CC:-clang}
    cxx=${CXX:-clang++}
    extld="$cc"
    sdk_path=${MACOS_SDK_PATH:-$(xcrun --sdk macosx --show-sdk-path)}
    ;;
  windows-amd64)
    goos=windows
    goarch=amd64
    output_name=beatportdl-windows-amd64.exe
    cc=${CC:-x86_64-w64-mingw32-gcc}
    cxx=${CXX:-x86_64-w64-mingw32-g++}
    extld="$cxx"
    cgo_cflags="${cgo_cflags} -DTAGLIB_STATIC -Wall -Wno-deprecated"
    cgo_cxxflags="${cgo_cxxflags} -DTAGLIB_STATIC -Wall -Wno-deprecated"
    extldflags="-static -static-libstdc++ -static-libgcc"
    buildmode_args=()
    ;;
  *)
    printf 'unsupported target: %s\n' "$target" >&2
    exit 1
    ;;
esac

if [ "$goos" = darwin ]; then
  sdk_frameworks=$(quote_cgo_arg "-F${sdk_path}/System/Library/Frameworks")
  sdk_libraries=$(quote_cgo_arg "-L${sdk_path}/usr/lib")
  cgo_ldflags="$cgo_ldflags $sdk_frameworks $sdk_libraries"
fi

cd "$repo_root"

# Go does not track changes to external libraries imported via cgo. Reuse
# a cache for identical native inputs without clearing the caller's cache.
native_digest=$(python3 - "$deps_prefix" <<'PY'
import hashlib
import sys
from pathlib import Path

prefix = Path(sys.argv[1])
digest = hashlib.sha256()
for name in ("include", "lib"):
    directory = prefix / name
    if not directory.is_dir():
        raise SystemExit(f"Missing native dependency directory: {directory}")
    for path in sorted(p for p in directory.rglob("*") if p.is_file()):
        content = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                content.update(chunk)
        digest.update(path.relative_to(prefix).as_posix().encode("utf-8"))
        digest.update(b"\0")
        digest.update(content.digest())
print(digest.hexdigest())
PY
)
cache_root=$(go env GOCACHE)
build_cache=$cache_root
if [ "$cache_root" != off ]; then
  build_cache="$cache_root/beatportdl-native/$target/$native_digest"
fi

if [ "$goos" = darwin ]; then
  export SDKROOT="$sdk_path"
fi

env \
  GOCACHE="$build_cache" \
  CGO_ENABLED=1 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  CC="$cc" \
  CXX="$cxx" \
  CGO_CPPFLAGS="$cgo_cppflags" \
  CGO_CFLAGS="$cgo_cflags" \
  CGO_CXXFLAGS="$cgo_cxxflags" \
  CGO_LDFLAGS="$cgo_ldflags" \
  go build -trimpath -buildvcs=false -mod=vendor "${buildmode_args[@]}" \
    -ldflags "-w -linkmode external -extld=${extld} -extldflags '${extldflags}'" \
    -o "${build_dir}/${output_name}" \
    "$build_src"
