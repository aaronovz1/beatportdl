# Changelog

## v1.0.10 - 2026-06-14

This hotfix release focuses on Windows release usability and more resilient CI/release builds.

### Fixed

- Statically link the Windows MinGW runtime so the published `beatportdl-windows-amd64.exe` no longer fails to start with a missing `libwinpthread-1.dll` error.
- Add a Windows release/CI guard that fails the build if the executable imports dynamic MinGW runtime DLLs such as `libwinpthread-1.dll`, `libstdc++-6.dll`, or `libgcc_s_seh-1.dll`.
- Harden Docker builder source downloads with bounded retries for apt, Zig, zlib, and TagLib fetches.
- Make the Docker builder select the correct pinned Zig tarball for both `amd64` CI runners and `arm64` local Docker builds.

## v1.0.9 - 2026-05-31

This release focuses on stabilization, downloader cleanup, and clearer user-facing behavior.

### Fixed

- Improved cross-platform path sanitization for filenames and directories, including Windows reserved names, dot-only names, and invalid trailing characters.
- Added deterministic handling for colliding track filenames so same-run duplicates get stable ` (1)`, ` (2)`, ... suffixes instead of corrupting downloads.
- Cleaned up leaked UUID-named temporary cover files across track, release, playlist, chart, label, and artist download flows.
- Clarified common 400/403/login/quality failures with more specific API error hints for authentication, subscription-tier mismatches, quality restrictions, and territorial availability.
- Clamped long progress-bar labels so extremely long track titles no longer overrun the terminal UI.
- Added retry logic around pinned dependency downloads and source clones to reduce flaky Windows CI failures during dependency bootstrap.

### Added

- Regression tests for collision handling, cover temp cleanup, API error classification, and progress-label clamping.
- Support guidance for common 400/403 causes in `SUPPORT.md`.

### Known Follow-Up

- Label query-parameter behavior remains intentionally scoped to release filtering, not per-track filtering inside matching releases.
- Path-reuse/redownload behavior after metadata or directory-name changes still needs a tight reproducible case before further changes.

## v1.0.8 - 2026-05-24

This is the first maintained-fork release from `aaronovz1/beatportdl`.

### Fixed

- Adopted upstream PR #100: playlist and chart `first_genre` directory templates now use the first listed track genre instead of playlist/chart metadata. Addresses upstream issue #99 in this fork.
- Adopted upstream PR #123: default M4A `track_bpm` mapping now uses `BPM_raw` so BPM values persist as MP4 freeform metadata.
- Made existing XDG path tests Linux-only so full local test runs pass on macOS when TagLib is installed.

### Added

- Maintained-fork README positioning and support policy.
- `CONTRIBUTING.md`, `SUPPORT.md`, and a release checklist in `docs/RELEASE.md`.
- Expanded issue templates for bug reports, feature requests, and questions.
- CI with pure Go tests plus a TagLib-enabled full test job.
- Regression tests for URL parsing, tag mapping defaults, path sanitization helpers, and first-genre directory naming.
- A pinned Docker builder, wrapper scripts, and CI that move supported release builds off host-managed Zig/TagLib/zlib installations.
- Native GitHub Actions release jobs for macOS amd64, macOS arm64, and Windows amd64, plus automatic GitHub release publishing with checksums.
