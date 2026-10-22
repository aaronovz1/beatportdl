# Release Checklist

This fork keeps the `beatportdl` name and publishes maintained-fork releases from `aaronovz1/beatportdl`.

## Versioning

- Use patch releases such as `v1.0.8` for compatibility-preserving fixes.
- Reserve minor releases such as `v1.1.0` for new CLI flags, config behavior, or workflow changes.
- Note upstream PRs and issues adopted in the changelog.
- Tag release candidates as `vX.Y.Z-rcN`. They should publish as GitHub prereleases, not as the latest stable release.

## Preflight

1. Confirm the working tree is clean except for intended release changes.
2. Fetch upstream:

   ```shell
   git fetch upstream
   ```

3. Confirm GitHub Actions runner coverage still matches the release targets:

   - `ubuntu-latest` for Linux container builds
   - `macos-15-intel` for `darwin-amd64`
   - `macos-15` for `darwin-arm64`
   - `windows-2022` with MSYS2 for `windows-amd64`

4. Build the pinned builder image:

   ```shell
   ./scripts/docker-build.sh build-image
   ```

5. Run build-policy regressions, workflow linting, and containerized tests:

   ```shell
   python3 -m unittest discover -s scripts/tests -v
   actionlint
   shellcheck scripts/docker-build.sh scripts/docker-builder-entrypoint.sh scripts/build-release-deps.sh scripts/build-release-target.sh
   ./scripts/docker-build.sh pure-test
   ./scripts/docker-build.sh test
   ```

6. Build the supported Linux release artifacts in the container:

   ```shell
   ./scripts/docker-build.sh release-core
   ```

7. Build macOS release artifacts if you have a locally licensed Apple SDK:

   ```shell
   MACOS_SDK_PATH=/path/to/MacOSX.sdk ./scripts/docker-build.sh darwin-amd64
   MACOS_SDK_PATH=/path/to/MacOSX.sdk ./scripts/docker-build.sh darwin-arm64
   ```

8. If Docker is unavailable, the equivalent host-managed smoke checks are:

   ```shell
   go test ./config ./internal/beatport ./internal/securefile ./internal/validator
   go test ./...
   ```

9. Smoke test one track, release, playlist, and chart with a valid subscription.
10. Verify tags, cover handling, filenames, progress output, and error logs.
11. Only when explicitly ready to publish, push the release tag and let `.github/workflows/release.yml` publish the GitHub release assets and `SHA256SUMS` after the runner jobs pass. Local build/test commands never dispatch a workflow or publish a release.
12. Ensure `CHANGELOG.md` contains the notes for the base version (`vX.Y.Z`), since the workflow publishes release notes from that section for both stable tags and `-rc` tags.

## Build Targets

The release should include:

- `beatportdl-darwin-amd64`
- `beatportdl-darwin-arm64`
- `beatportdl-linux-amd64`
- `beatportdl-linux-arm64`
- `beatportdl-windows-amd64.exe`

Linux targets are expected to come from the pinned Docker builder. macOS targets are expected to come from native GitHub macOS runners. Windows targets are expected to come from a native GitHub Windows runner with MSYS2 MinGW.

## Supply-Chain Policy

The Go toolchain is 1.27.3. TagLib is 2.3.2 at official commit
`deadc2990767dfbda0701e0ab35fdeea653db08f`. Zig remains 0.16.0 and zlib
remains 1.3.2 with separate SHA-256 pins for the official and GitHub archives.
The Go module graph is vendored; compilation does not fetch modules or an
automatic replacement toolchain. Policy tests parse workflows with the existing
vendored `go.yaml.in/yaml/v3` dependency, so they require Go 1.27.3 and a
synchronized vendor tree, not an additional Python YAML package.

Pins were resolved from official metadata on 2026-10-22:

- [TagLib v2.3.2 ref](https://api.github.com/repos/taglib/taglib/git/ref/tags/v2.3.2), peeled to its commit.
- [Docker Registry API](https://distribution.github.io/distribution/spec/api/): the multi-platform index for `golang:1.27.3-bookworm` is `sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5`; `docker/dockerfile:1.7` is `sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e`.
- Official stable release refs and each pinned `action.yml` for [checkout v7.0.1](https://api.github.com/repos/actions/checkout/git/ref/tags/v7.0.1), [setup-go v7.0.0](https://api.github.com/repos/actions/setup-go/git/ref/tags/v7.0.0), [upload-artifact v7.0.2](https://api.github.com/repos/actions/upload-artifact/git/ref/tags/v7.0.2), [download-artifact v8.0.2](https://api.github.com/repos/actions/download-artifact/git/ref/tags/v8.0.2), [attest-build-provenance v4.2.2](https://api.github.com/repos/actions/attest-build-provenance/git/ref/tags/v4.2.2), and [setup-msys2 v2.33.0](https://api.github.com/repos/msys2/setup-msys2/git/ref/tags/v2.33.0). Workflows contain full resolved commit IDs, never mutable refs. JavaScript actions declare Node24 directly; the composite attestation action pins `actions/attest@508db95dd578ae2727ebd6217d5ba78e4fbda05d`, whose `action.yml` also declares Node24. No forced-runtime override substitutes for upgrading old action majors.
- [actionlint v1.7.12 assets](https://api.github.com/repos/rhysd/actionlint/releases/tags/v1.7.12) provide the SHA-256 used by the workflow lint gate.

Checksums compare downloaded bytes to reviewed pins. Commit checks verify the
expected Git object identity, including submodule commits. Neither is upstream
signature verification: the build does not import maintainer keys or verify
detached archive signatures or signed Git tags/commits. HTTPS and official
metadata are the trust bootstrap for these pins. Dependabot proposes action and
Docker updates for review; it does not authorize publication.

Native zlib archives are checked on every invocation and always re-extracted;
an extracted source cache is never trusted. TagLib caches must have the exact
commit and pristine tracked, untracked, ignored, and recursive submodule trees.
Raw blob hashes are checked independently of Git index flags. Tampered caches
fail rather than being silently reset. Dependency URL/version/hash environment
overrides are trusted-operator inputs for local testing, not release inputs.
Git replacement processing is disabled so local replacement objects cannot
substitute a different tree behind a pinned commit. Native macOS link paths
prefer the pinned dependency prefix over the SDK's libraries.

## Docker Boundary

The Docker build wrapper requires host Git and Python 3 in addition to a running
Docker engine; they create the secret-filtered working-tree snapshots before
anything is sent or mounted into Docker.

`scripts/docker-build.sh` creates two disposable snapshots. The image context
contains only `Dockerfile.build` and its three helper scripts. The runtime
snapshot selects working-tree files reported by `git ls-files --cached --others
--exclude-standard`, including local edits, deletions, and unignored new files.
Only the Makefile, module manifests, Go/native sources under `cmd`, `config`,
`internal`, and `vendor`, `vendor/modules.txt`, and test fixtures are included.
Hidden files/directories, configuration and credential files, private keys,
logs, `.git`, and `.third_party` are excluded even if tracked. Source symlinks
and symlinked parent directories are rejected. Credentials must never be placed
inside source code or test fixtures. `.dockerignore` is defense in depth, not
the runtime protection mechanism.

The source is mounted read-only at `/workspace`, output separately at `/output`
(host `bin/`, or `DOCKER_OUTPUT_DIR`). Compilation/tests use `--network=none`,
`--cap-drop=ALL`, `no-new-privileges`, a read-only root filesystem, and the host
UID/GID. Writable dependency builds, Go/Zig caches, and temporary files live in
an ephemeral `/tmp` mount. An optional licensed macOS SDK is also read-only.
No Makefile change is needed: the entrypoint supplies `make BUILD_DIR=/output`.

Image creation needs network access for official images, Debian packages,
verified Zig/zlib archives, and pinned TagLib/submodules. Native dependency
preparation and CI tool setup also need network access. Only the compile/test
container is offline; this is not a claim that dependency acquisition is
hermetic. Debian, Homebrew, MSYS2 packages, and hosted runner images can change,
so recorded tool versions do not imply bit-for-bit reproducible binaries.

## Release Gates

Both tag pushes and manual dispatch require a `v`-prefixed SemVer tag. Dispatch
from a branch fails before building; invalid tags do too. Every release build
depends on policy checks and a full offline test run at the exact checked-out
`github.sha`. All checkouts disable persisted credentials. Default permissions
are `contents: read`; only the publisher gets `contents: write`. A separate
attestation job alone gets `id-token: write` and `attestations: write` and must
pass before publication. Existing changelog-derived notes and RC prerelease/
not-latest behavior are retained.

The native macOS arm64 CI and release jobs run `govulncheck ./...` after building
TagLib dependencies and before building the application. They install
`golang.org/x/vuln/cmd/govulncheck@v1.9.0`, use Go 1.27.3 with
`GOTOOLCHAIN=local`, and supply CGO header/library paths for the native dependency
prefix so all packages can be analyzed. Installation and the official Go
vulnerability database at `https://vuln.go.dev` require network access outside
the offline Docker boundary. Scanner failures block those jobs and publication;
this is Go vulnerability analysis, not a scan of native TagLib/zlib source.

The Windows 2022 CI and release jobs execute the securefile, config, Beatport,
and validator tests natively before packaging, including Windows DACL/reparse
regressions. Cross-compilation or a macOS/Linux pass is not evidence that those
Windows-specific tests pass.

Each platform uploads toolchain/native-dependency versions and embedded Go
module build information. The attestation job generates platform-specific
CycloneDX dependency SBOMs from those inventories and uploads them before
attesting the archives and metadata.
The SBOM covers linked Go modules plus TagLib/zlib, not a complete hosted-runner
or operating-system inventory. The container additionally records Debian
package versions. `SHA256SUMS` covers archives and metadata. GitHub build
provenance attestations bind those assets to this workflow; they do not replace
upstream signature verification, independent review, or macOS code signing/
notarization. No new signing or notarization infrastructure is required here.

## v1.0.8 Candidate Notes

Include these highlights if present in the release:

- Maintained fork documentation and support policy.
- Upstream PR #100: playlist/chart `first_genre` uses the first listed track genre.
- Upstream PR #123: default M4A BPM tag mapping writes through `BPM_raw`.
- Dockerized Linux CI plus native macOS and Windows release publishing through GitHub Actions.
