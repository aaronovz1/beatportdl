# Changelog

## v1.0.8 - Unreleased

This is the first maintained-fork release candidate from `aaronovz1/beatportdl`.

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
