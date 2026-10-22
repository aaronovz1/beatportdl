# v1.2.0 Release Preflight

Status: local automated checks passed. On 2026-10-22 the maintainer reported
successful testing and explicitly authorized committing the notes and publishing
`v1.2.0`, reflecting Beatport-only support and configuration/security behavior
changes. Publication is subject to CI and release-workflow gates below.

Checked on 2026-10-22 against runtime/build commit
`d4d1cec0c23250ddc4f91f7aa15848e8f2928392`. The only tracked changes made during
this preflight are release documentation. No tag, commit, push, release, or
publishing workflow dispatch was performed.

## Verified

- GitHub [CI run 39814023366](https://github.com/aaronovz1/beatportdl/actions/runs/39814023366) completed successfully at the exact commit above: build policy, native Windows, native Intel/arm64 macOS, Docker tests, and Docker cross-builds.
- Fresh local `go test -count=1 -mod=vendor -race ./...` and full offline Docker tests passed. Tests use fixtures/mock servers, not a live account.
- All 19 build-policy tests, actionlint, and build-script syntax checks passed locally. ShellCheck passed in the CI policy job; it was not installed or rerun on this host.
- Fresh `govulncheck` using Go 1.27.3, scanner v1.9.0, and the official database reported no vulnerabilities. This does not scan native TagLib/zlib code.
- Upstream refs were refreshed without merging. Upstream head remains `878d3f6`; optional upstream feature ports are not included in this release.
- Five fresh executable archives have the expected architecture and contain exactly one expected executable, with no config, credential, log, or extra file entries. Unix archives preserve mode `0755`; extracted bytes match the inspected build artifacts.
- Both extracted macOS executables passed strict code-signature verification and `--help` (Intel via Rosetta). Both link only Apple system libraries, without dynamic TagLib/zlib/Homebrew dependencies. These local candidates target macOS 15.0 and use SDK 26.2; they are ad-hoc signed, not notarized.
- Both extracted Linux executables passed `--help` in offline containers (Intel under emulation). ELF dependencies are system glibc libraries; TagLib/zlib are not external runtime dependencies. Other Linux distributions were not exercised.
- The extracted Windows executable passed the runtime import guard: no `libwinpthread`, `libstdc++`, or `libgcc` DLL dependency. Native Windows filesystem/auth/config regressions passed in CI; the locally cross-built executable was not executed on Windows.
- The release workflow's inventory, SBOM, and checksum generation was rehearsed without executing its publishing or attestation steps. Four CycloneDX dependency SBOMs each contain 16 components, covering every embedded Go module plus TagLib 2.3.2 and zlib 1.3.2. All 13 checksum entries verified from a flat download directory with unique filenames.
- Stable and RC release-note extraction works without creating tags. All prior unreleased changelog entries are preserved, with upgrade guidance added and no personal playlist examples.
- A fresh arm64 candidate was installed at `~/beatportdl-darwin-arm64`. It matches the extracted build artifact byte-for-byte, passes strict signature verification, and starts with `--help`. Config, credentials, and other home-directory copies were not changed.

## Local Candidate Assets

All staging is under ignored `bin/release-preflight/v1.2.0/`. These are local
preflight artifacts, not official GitHub release assets, and must not be uploaded
as replacements for the native release-runner output.

| Archive | Build origin | Compressed Bytes |
| --- | --- | ---: |
| `beatportdl-v1.2.0-darwin-amd64.tar.gz` | Apple clang cross-build on arm64; Rosetta startup check | 4,242,766 |
| `beatportdl-v1.2.0-darwin-arm64.tar.gz` | Native Apple clang build | 3,831,271 |
| `beatportdl-v1.2.0-linux-amd64.tar.gz` | Hardened Docker/Zig cross-build | 4,280,533 |
| `beatportdl-v1.2.0-linux-arm64.tar.gz` | Hardened Docker/Zig build | 3,940,502 |
| `beatportdl-v1.2.0-windows-amd64.zip` | Hardened Docker/Zig cross-build; equivalent ZIP layout, not PowerShell packaging | 4,191,787 |

The `downloads/` subdirectory contains the five archives, four tool inventories,
four SBOMs, and `SHA256SUMS`. `evidence/package-checks.json` records archive and
executable hashes. `evidence/` also contains the build/test/runtime logs. Local
tool inventories can contain local build paths; they are not for publication.
`release-notes.md` is generated from the proposed version section in
[CHANGELOG.md](../CHANGELOG.md).

## Release Handoff

The maintainer's testing approval is recorded above; its full platform/scenario
coverage was not independently observed. The first three items remain the
end-to-end smoke-test checklist, not additional automated test claims.

1. Live-account smoke tests on macOS and Windows: one track, release, playlist, chart, artist, and label; include direct media and HLS with FFmpeg. Verify tags, covers, filenames, and player loading. No credentials or signed media URLs should appear in shared test output.
2. Exercise a no-op update, a metadata/filename change, cancellation, and an overwrite failure using disposable test downloads; verify no duplicates, UUID leftovers, lost originals, or prematurely removed shared directories. Automated regressions passed, but live end-to-end behavior was not tested during this preflight.
3. Confirm first-run setup and existing private config/token/log files on each user platform. Deliberately shared or symlinked locations may now fail safely; do not weaken permissions to work around this.
4. Version and notes are approved and the version heading is dated 2026-10-22. Commit/push the intended documentation, then recheck CI on that exact commit before creating the authorized `v1.2.0` tag.
5. Publication is authorized: let the release workflow build native Windows/macOS assets, package Windows with PowerShell, issue GitHub provenance attestations, and publish. Recheck the actual downloadable archives, inventories, SBOMs, notes, and checksum manifest after that run. No such native packaged assets or provenance attestations were available from ordinary CI during the local preflight.

No new automated release blocker was found. This records the local preflight
and the maintainer's testing/publication approval; successful release-workflow
execution and published-asset verification must be confirmed separately.
