# Changelog

## Unreleased

### Fixed

- Defer empty-directory cleanup until all active URL workers finish, preventing a failed or completed URL from removing a folder another download still needs. Serialize final cleanup with new URL admission, drain it after cancellation/panic too, and remove requested empty children before parents. Preserve the configured download root (including path aliases), nonempty directories, regular files and symlinks. Temporary audio/cover cleanup remains immediate.
- Reject malformed HTTP-success catalog metadata and collection pages instead of treating them as valid resources or empty results. Require positive resource IDs and explicit results arrays, validate collection item identities and pagination values, and reject trailing JSON. Preserve valid empty pages, optional/unknown metadata, HTTP errors and response-body cleanup.
- Reject negative search-result selections instead of panicking, while preserving valid track/release selections and their requested order.
- Return validation errors for incomplete library and localized catalog URLs instead of indexing missing URL components.
- Reject unsafe `whitespace_character` values from config files and CLI overrides. Sanitize substituted path components again so replacement text cannot introduce directory traversal, forbidden filename characters or reserved names; preserve intentional directory-template separators.
- Decode JSON escapes in track/release names correctly, including Unicode, quotes, slashes and literal backslashes. Normalize decoded whitespace and reject invalid non-string values without changing the previous name. Existing identity-matched files keep their filenames when decoded names differ.
- Refresh duplicate-detection indexes for each download run, record completed tracks immediately, and revalidate cached files after deletion or replacement. Filename corrections no longer cause repeat downloads within a run; file timestamp changes alone do not make a matching track different.
- Reuse existing identity-matched files, including collision-suffixed names, before allocating a new filename. Preserve `track_exists` policies, audio-format separation and ownership of paths reserved by other active tracks.
- Bound catalog authentication recovery to one refresh/replay, preserve replayed request bodies and final API errors, and close rejected responses before retrying.
- Synchronize token snapshots, serialize refresh/login without blocking cancelled waiters, reuse concurrent refreshes, keep authorization cookies request-local, and close login/authorization responses.
- Join playlist, chart and catalog download workers before returning on pagination or processing errors, preventing premature progress shutdown and cover cleanup.
- Cancel queued workers, in-flight API/media requests, retry waits and FFmpeg operations on shutdown. Clean interrupted staging files without replacing existing tracks; bound media response headers to 40 seconds and allow up to 30 minutes for each media transfer.
- Fix Dockerized Windows cross-builds by using Zig's target-aware archive tools for TagLib and zlib. Host archive tools could omit the Windows symbol indexes and cause unresolved TagLib symbols at link time. Add a Windows Docker build and runtime-DLL check to CI.
- Preserve existing audio files when an overwrite download or HLS remux fails. Only completed transfers replace the destination.
- Close and remove partial HLS staging files after failures instead of leaving UUID-named files in download directories.
- Reject malformed HLS encryption IVs, ciphertext lengths, and padding with errors instead of panics or truncated output.
- Reject empty or structurally invalid direct FLAC/M4A downloads before publishing files, even when tag fixes are disabled, using the existing TagLib dependency. This checks containers and audio properties, not a full audio decode. Reject empty HLS streams and remux output too.
- Use short, bounded staging filenames so valid long track filenames can still be downloaded and remuxed.

### Added

- Synchronized regressions for concurrent track URLs sharing a genre folder, cleanup after success/panic/cancellation, cancelled admission and fresh runs, nested empty directories, root aliases, completed files and symlinks.
- Request-count regressions and a no-op playlist benchmark, plus coverage for shared release metadata across pages, fresh-run retries, server-selected formats, denied access, HLS skipping, filename fallbacks, release/genre directory layouts, metadata-failure cleanup and independent chart covers. Add 10,000-job scheduling regressions for bounded goroutines, cancellation, worker joins and panic recovery.
- Catalog response regressions for missing/null/invalid metadata, malformed pages and playlist items, valid empty pages, pagination/result preservation, HTTP errors, read failures and response-body closure across all catalog metadata getters.
- Regression tests for invalid search selections, incomplete and supported URL routes, safe/unsafe whitespace replacements and overrides, JSON string decoding, and legacy-filename identity matching for FLAC/M4A.
- Duplicate-detection regressions using real FLAC/M4A tags for completed downloads, fresh runs, removed/replaced files, timestamp-preserving tag edits, no-op updates, unchanged identities, collision suffixes, concurrent reservations and indexed tag-read counts.
- Regression tests for persistent 401s, payload replay, concurrent auth/cache operations, response-body closure, cancellation, media timeouts, FFmpeg shutdown and collection-worker completion.
- Regression tests with synthetic audio for failed and successful overwrites, partial-download and descriptor cleanup, malformed HLS data, invalid direct media, empty responses, long destination filenames, normal HLS output permissions, and direct downloads without FFmpeg.

### Removed

- Remove standalone Beatsource API clients, URL/store routing, and search selection in line with upstream's removal and Beatport's announced migration. Downloads and searches now use Beatport only.
- Remove Beatsource support claims, obsolete subscription examples, and the multi-store issue field from the README, support/contribution guides, and issue templates.

### Changed

- Reuse release metadata within each playlist/chart download. Under `track_exists: skip`, identity-matched files in the server-selected audio format skip additional release/full-track lookups where the destination is already known. Keep per-track subscription/access and format checks, filename fallback and full-track numbering; do not reuse media URLs across tracks or runs. Only fetch the first track's genre when a directory template uses `{first_genre}`. Defer directory cleanup until all sibling workers finish and preserve chart covers when release-cover requests fail.
- Apply backpressure before spawning URL/download workers so large collections no longer create a waiting goroutine for every queued item. Worker limits, cancellation and error-path joins remain enforced.
- Reject old Beatsource URLs and `@beatsource` queries with migration guidance instead of silently treating them as Beatport resources. Use migrated Beatport URLs; existing config, credentials, and downloaded files are left unchanged.
- Restore live console logging at each interactive prompt so migration errors remain visible after a previous download's progress display has shut down. Add tests for Beatport-only requests and resource links, removed inputs, and repeat-prompt guidance.

## v1.1.1 - 2026-10-19

This release improves label and artist filtering and download organization, while preserving existing-file detection and cover cleanup.

### Changed

- Adopt upstream's general track endpoint for label and artist downloads. Label filters now select individual matching tracks instead of downloading every track on a matching release.
- Share release metadata and lazy covers across catalog tracks while preserving metadata-based duplicate detection, filename collision handling, and UUID cover cleanup. Wait for active workers before cleanup even when pagination fails.
- Group context-sorted artist and label downloads beneath top-level `Artists` and `Labels` directories, preserving their configured directory templates. Both put tracks directly in their context directory by default; release subdirectories are opt-in through `force_release_directories`. Keep embedded covers, but avoid conflicting `cover.jpg` files in flat artist/label folders. Existing files are not moved automatically.
- Preserve the selected label/artist and pagination when forwarding URL filters, reject malformed query parameters, and retain the selected store on returned tracks.

### Added

- Regression tests for scoped catalog requests, filter forwarding, pagination, grouped directory templates, flat destination overrides, shared covers, embedded cover tagging, ISRC-based skipping after metadata changes, filename collisions, and error-path cleanup.

### Existing Downloads

With `sort_by_context: true`, the default layout is now:

```text
<downloads>/Artists/<artist directory template>/track files
<downloads>/Labels/<label directory template>/track files
```

Existing files are not migrated or searched recursively. Move existing artist/label context folders beneath the new `Artists`/`Labels` groups before rerunning. If they contain release subdirectories, enable `force_release_directories: true` to retain that layout, or move the audio files into their context directory. Otherwise, tracks in the old location may be downloaded again.

Set `sort_by_context: false` (or `--sort-by-context=false`) to download directly into `downloads_directory` without groups or release subdirectories. Other URL types keep their existing layout.

## v1.1.0 - 2026-10-14

This release adds command-line configuration overrides and adopts targeted upstream improvements to HLS downloads and allocation efficiency.

### Added

- Override config values for a single run using command-line flags, including download destinations, quality, templates, booleans, concurrency, and tag mappings. Overrides are validated before login and leave the config file unchanged.
- Preserve configured values for omitted flags, accept explicit false, zero, and empty-string overrides, and use the last value when a flag is repeated.
- Show `--help` before configuration setup or authentication, and report malformed flags before starting setup.
- Add regression coverage for config precedence, validation, tag mappings, CLI startup, and HLS response cleanup, plus allocation benchmarks.

### Fixed

- Adopt upstream PR #126's HLS cleanup fix: close each segment response body before requesting the next segment instead of deferring closure until the full track finishes.
- Reject zero or negative worker counts during config validation to prevent stalled downloads or invalid worker pools.
- Publish portable checksum manifests with download filenames, excluding the manifest's own checksum.

### Changed

- Selectively port upstream PR #126's allocation optimizations by reusing the compiled template regexp and JSON whitespace cleanup replacer, while preserving this fork's path sanitization and directory handling.
- Document the local macOS arm64 test-build installation workflow for future contributions.

### Command-Line Usage

Use config option names with underscores replaced by hyphens. Put flags before URLs or text files:

```shell
./beatportdl -q --downloads-directory "/Music/Playlist" --sort-by-context=false --track-exists=skip tracks.txt
./beatportdl --quality=high --max-download-workers=4 --track-number-padding=0 tracks.txt
```

Use `=false` to disable a boolean option. `--tag-mappings` accepts a quoted YAML or JSON map and replaces the configured map. Run `./beatportdl --help` for all options. First-run configuration creation still uses setup prompts.

## v1.0.13 - 2026-09-27

This hotfix release improves download reliability and standalone track organization.

### Fixed

- Avoid download-worker panics while checking existing audio files that do not contain the configured track identity tags.
- Retry transient API timeouts for safe read requests instead of aborting playlist and other catalog downloads after the first stalled response.
- Store standalone track downloads in their genre directory instead of a catalog-number-prefixed release directory.

## v1.0.12 - 2026-06-23

This hotfix release focuses on safer downloads and duplicate detection when different tracks resolve to the same filename.

### Fixed

- Avoid accepting partial or corrupt downloads by staging files through temporary paths, checking HTTP download sizes when available, and validating HLS-remuxed M4A files before moving them into place.
- Return a clear HLS stream error instead of panicking when a playlist segment is missing encryption-key metadata, and include stack traces for any recovered worker panic.
- Avoid treating different tracks as duplicates when artist abbreviation or metadata produces the same computed filename; existing files with different embedded track identity now get a suffixed filename instead of being skipped or updated.

## v1.0.11 - 2026-06-14

This hotfix release focuses on no-op reruns after tracks already exist locally.

### Fixed

- Avoid duplicate downloads when current Beatport metadata produces a different filename for an already-downloaded track by matching existing same-format files in the target directory by embedded track identity tags.
- Avoid downloading and leaving temporary UUID-named cover files when a track is skipped because it already exists on disk.

## v1.0.10 - 2026-06-14

This hotfix release focuses on directory-template correctness, Windows release usability, and more resilient CI/release builds.

### Fixed

- Statically link the Windows MinGW runtime so the published `beatportdl-windows-amd64.exe` no longer fails to start with a missing `libwinpthread-1.dll` error.
- Preserve explicit directory separators in directory templates, fixing `first_genre` playlist/chart folders being flattened into one sanitized directory instead of a genre directory followed by the playlist or chart name.
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
