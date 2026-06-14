#!/usr/bin/env bash
set -euo pipefail

target=${1:?usage: build-release-target.sh <target> <deps-prefix>}
deps_prefix=${2:?usage: build-release-target.sh <target> <deps-prefix>}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

build_dir=${BUILD_DIR:-"$repo_root/bin"}
build_src=${BUILD_SRC:-./cmd/beatportdl}

mkdir -p "$build_dir"

goos=
goarch=
output_name=
cc=
cxx=
cgo_cppflags="-I${deps_prefix}/include"
cgo_cflags="-I${deps_prefix}/include"
cgo_cxxflags="-I${deps_prefix}/include"
cgo_ldflags="-L${deps_prefix}/lib"
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
    export SDKROOT="$sdk_path"
    cgo_ldflags="-F${sdk_path}/System/Library/Frameworks -L${sdk_path}/usr/lib ${cgo_ldflags}"
    ;;
  darwin-amd64)
    goos=darwin
    goarch=amd64
    output_name=beatportdl-darwin-amd64
    cc=${CC:-clang}
    cxx=${CXX:-clang++}
    extld="$cc"
    sdk_path=${MACOS_SDK_PATH:-$(xcrun --sdk macosx --show-sdk-path)}
    export SDKROOT="$sdk_path"
    cgo_ldflags="-F${sdk_path}/System/Library/Frameworks -L${sdk_path}/usr/lib ${cgo_ldflags}"
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

if [ -z "$extld" ]; then
  extld="$cc"
fi

cd "$repo_root"
go clean -cache

env \
  CGO_ENABLED=1 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  CC="$cc" \
  CXX="$cxx" \
  CGO_CPPFLAGS="$cgo_cppflags" \
  CGO_CFLAGS="$cgo_cflags" \
  CGO_CXXFLAGS="$cgo_cxxflags" \
  CGO_LDFLAGS="$cgo_ldflags" \
  go build "${buildmode_args[@]}" \
    -ldflags "-w -linkmode external -extld=${extld} -extldflags '${extldflags}'" \
    -o "${build_dir}/${output_name}" \
    "$build_src"
