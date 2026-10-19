# Authentication and Worker Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Bound authentication recovery and make cancellation and collection-worker shutdown reliable without changing download organization or access requirements.

**Architecture:** Keep the existing Beatport client API, adding an immutable request-context view. Serialize refresh/login independently of token snapshot locking. Join collection workers through a shared pagination boundary; use context-aware worker slots and bounded media HTTP requests.

**Tech Stack:** Go, standard context/net/http/sync packages, existing pinned native dependencies and Docker builder.

**Spec:** User-approved next-step proposal in this chat; validated review findings R02-R04, R07 and R16 in `/private/tmp/beatportdl-review-75d66e6-report.txt`.

## Global Constraints

- No release. The completed file-safety and cross-build checkpoints were committed and pushed separately. After local testing passed, the user authorized committing and pushing the auth/lifecycle batch too.
- Preserve existing configuration, credentials, naming, cover cleanup and duplicate detection. No access-control bypasses or new dependencies.
- Use synthetic tokens, local fixture servers and temporary caches only.
- Refresh `$HOME/beatportdl-darwin-arm64` from the verified tree; compare bytes and verify its signature.

## Review Focus

- Persistent 401s and concurrent refresh must terminate without leaking bodies or replaying a request with a consumed payload.
- Cancellation during refresh must not trigger fallback login or leave a lock held.
- Pagination errors and cancellation must join workers before progress/cover/directory finalization.
- Failed/cancelled audio, covers and HLS transfers must retain old destinations and clean temporary files.
- Long legitimate media transfers must not inherit the catalog client's short overall deadline.

### Task 1: Checkpoint Completed Fixes

- [x] Run full native race tests and build-script checks.
- [x] Commit file safety separately from Docker cross-build changes, including their own changelog entries.
- [x] Push `ae5141b` and `b9e73ac` to main; inspect native Windows/macOS CI run `39280525190`.
- [x] Confirm CI finishes successfully; diagnose failures before reporting completion.

### Task 2: Authentication and API Cancellation

**Files:** `internal/beatport/auth.go`, `beatport.go`, and package tests.

**Interfaces:** Preserve existing requests; add `WithContext(context.Context) *Beatport` for a shallow context-bound client view. Parent wires main's application context into this view.

- [x] Reproduce persistent 401 body leaks, refresh races/stampedes, auth-response leaks and request cancellation with synthetic transports/caches.
- [x] Permit one authentication recovery/replay, preserve POST payloads and final API errors, close rejected and successful auth bodies.
- [x] Synchronize token snapshots and refresh/login without nested-lock deadlocks or shared cookie/header mutation; avoid fallback login after cancellation.
- [x] Run `go test -race -count=1 ./internal/beatport`; record red/green evidence.

### Task 3: Worker and Media Lifecycle

**Files:** `cmd/beatportdl/utils.go`, `main.go`, `needledrop.go`, `downloader.go`, `catalog.go`, and focused lifecycle tests.

**Interfaces:** Shared `forPaginatedWorkers` owns joining a caller-supplied wait group before returning. `application.semAcquire` reports cancellation. Media requests use an injectable internal HTTP client and the application context; HLS playlist/key fetching becomes an application method.

- [x] Add failing queued-worker, media-cancellation and timeout tests; prove queued work can stop without opening an occupied slot.
- [x] Implement context-aware slot acquisition for global and download workers, retaining wait-group and panic cleanup.
- [x] Use a shared media client with a 40-second response-header timeout and 30-minute overall deadline; cancel active audio, cover, playlist, key and segment reads on application cancellation.
- [x] Join playlist, chart and catalog workers at the shared pagination boundary for success, fetch errors and processing errors.
- [x] Recheck cancellation before publishing media or starting another HLS request; preserve destination/staging safety.
- [x] Run focused native race tests and existing cover/identity regressions.

### Task 4: Integration and Handoff

- [x] Review combined patch in fresh context; repair important findings with regression tests.
- [x] Run full native race tests, vet, Docker tests and representative Windows build/dependency checks.
- [x] Update Unreleased notes and cancellation/timeout documentation; no version or release tag.
- [x] Build/install native macOS arm64, compare artifact and verify codesign; preserve other home files.
- [x] Report checkpoint CI, new uncommitted fixes and any remaining limitations.

## Progress

- Checkpoints pushed to main. New implementation isolated on `codex/auth-worker-lifecycle`.
- Auth/API implementation delegated with disjoint ownership; parent owns worker/media lifecycle and integration.
- Ruling: cancel in-flight media on the first shutdown signal, discard staging data, and join workers. Allow 30 minutes for large media bodies, with a separate 40-second header bound. Duplicate-cache and queue-size performance work remain separate.
- Native full-tree `go test -race -count=1 ./...` passed with pinned TagLib flags. Auth package also passed 25 race-test iterations independently. Red evidence: queued-worker/media cancellation, early pagination return and stalled FFmpeg tests all failed before their changes.
- Ruling: all three users of the old pagination helper queue workers, so replace it with `forPaginatedWorkers` rather than adding a second wrapper. Its explicit wait-group parameter makes completion ownership unambiguous.
- Checkpoint GitHub CI remains queued; local verification continues independently. Do not report hosted CI as passed without its completion result.
- Final review corrections: share failed renewal results among gate waiters, exclude another caller's cancellation, and do not share success for a token rejected during its own publication. Regression tests reproduced 9/9 refresh/login attempts and a skipped required refresh before the respective fixes. Final auth race suite passed 20 iterations; full native race tests, vet, Docker suite and Windows cross-build/DLL checks passed.
- The local CI watcher was stopped without cancelling the queued GitHub run. Native hosted Windows/macOS confirmation is still pending.
- Final native checkpoint results: Windows amd64, macOS arm64, macOS amd64 and Dockerized tests all passed. The workflow API reported success; the last job-list snapshot still showed Docker cross-builds finishing, so check that job separately before treating all hosted validation as complete.
- Final macOS test build installed at `/Users/aaron/beatportdl-darwin-arm64`, byte-identical to the artifact, codesign valid, `--help` smoke test passed. SHA256: `4299427440054fde6ea14aa0732eb81a0ee89036d7aec357b7f722f85515f718`. Config, credentials and other copies preserved. Auth/lifecycle remains uncommitted on `codex/auth-worker-lifecycle`; no release.
- Final hosted confirmation: Dockerized cross-builds completed successfully. All jobs in checkpoint run `39280525190` passed. This hosted run validates the two pushed checkpoints, not the new uncommitted auth/lifecycle batch.

## Integration Approval

- User testing passed; commit and push approved, with no release.
- Re-ran the complete native race suite before committing: all packages passed. The initial sandboxed attempt could not bind a fixture HTTP server; the same command passed with loopback access enabled, without code changes.
- The installed test binary still matches the build artifact and passes strict codesign verification. No production code changed after user testing.
- Next review batch: R05/R06, duplicate-index freshness and reuse of existing suffixed tracks. Input/serialization robustness and request-count/queue performance follow afterward.
