# v1.2.1 Release Preflight

Checked on 2026-11-22 against runtime/build commit
`1528adae15c961a147131b014fc09096cda02ee1`. The only tracked preflight
changes are the undated release notes and this record. No commit, push, tag,
release, or publishing workflow dispatch was performed.

## Verification

- [GitHub CI run 45022689108](https://github.com/aaronovz1/beatportdl/actions/runs/45022689108)
  succeeded on the exact commit: build policy, Dockerized tests, both native
  macOS builds, native Windows, and Docker cross-builds. The native Windows
  regressions, executable build, and DLL import guard all passed. CI also ran
  actionlint and ShellCheck.
- Fresh local race-enabled Go suite, all 28 offline build-policy tests, and the
  pinned Dockerized Go suite passed with fixtures, not a live account.
- Fresh Go vulnerability analysis with Go 1.27.4, govulncheck v1.9.0, and the
  official database reported no vulnerabilities. This is not a vulnerability
  scan of TagLib or zlib.
- Refreshed upstream references without merging. Upstream main remained
  `878d3f6`. Reviewed the complete three-commit range since `v1.2.0` and
  generated both stable and RC draft notes from the exact changelog heading.

## Packaged Candidates

All five archives contain exactly their expected executable; extracted bytes
match the build outputs. Unix archive extraction preserves mode `0755`. The
candidates, logs, and build caches are on the external development volume.
These are preflight artifacts, not release-runner artifacts.

| Archive | Build and inspection | Bytes |
| --- | --- | ---: |
| `beatportdl-v1.2.1-darwin-arm64.tar.gz` | Native Apple clang; ad-hoc signature verified; extracted `--help` passed | 3,866,380 |
| `beatportdl-v1.2.1-darwin-amd64.tar.gz` | Intel cross-build on arm64; unsigned; extracted `--help` passed under Rosetta | 4,198,265 |
| `beatportdl-v1.2.1-windows-amd64.zip` | Docker/Zig cross-build, local ZIP with the release layout; archive integrity and x86-64 PE imports verified | 4,235,882 |
| `beatportdl-v1.2.1-linux-amd64.tar.gz` | Docker/Zig cross-build; extracted `--help` passed under emulation in an amd64 Debian container | 4,318,796 |
| `beatportdl-v1.2.1-linux-arm64.tar.gz` | Docker/Zig build; extracted `--help` passed in an arm64 container | 3,971,650 |

Both local macOS candidates target macOS 15.0, were built with SDK 26.2, and
link only Apple system libraries. Neither is Developer ID signed or notarized.
The Intel result is from the unchanged cross-build output, not evidence of the
signature on a future native Intel release-runner asset.

The Windows ZIP contains one x86-64 console executable. The extracted binary
has Windows system runtime imports and no `libwinpthread-1.dll`,
`libstdc++-6.dll`, or `libgcc_s_seh-1.dll` dependency. It was not executed on
Windows. Linux candidates link system glibc libraries, not dynamic TagLib or zlib.

The workflow's dependency SBOM and checksum generation was rehearsed locally.
Four CycloneDX SBOMs each contain 16 components. All 13 manifest entries
verified: five archives, four inventories, and four SBOMs.

## Native Intel Signing Follow-Up

Signing workflow commit `f03a046` adds explicit ad-hoc signing and strict
verification to the native Intel CI and release jobs. The release job also
verifies the executable again after archive extraction.
[The native Intel CI job](https://github.com/aaronovz1/beatportdl/actions/runs/45025539411)
reported `Mach-O thin (x86_64)` and `Signature=adhoc`; strict verification
and `--help` passed. The added archive step was rehearsed locally with an
ad-hoc-signed Intel cross-build. This is not Developer ID signing or notarization.

## Before Publishing

- Confirm CI on the exact release notes commit before tagging.
- The maintainer reported successful local testing. Live-account smoke tests
  were not independently repeated for this preflight. A native Windows
  startup/download smoke test remains separate from the CI regressions and
  local cross-build checks.
- When authorized, let the release workflow create the official native
  packages, generate provenance attestations, and publish. Recheck the actual
  published archives and metadata afterward. Do not substitute these local
  candidates for those artifacts.
