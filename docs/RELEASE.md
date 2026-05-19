# Release Checklist

This fork keeps the `beatportdl` name and publishes maintained-fork releases from `aaronovz1/beatportdl`.

## Versioning

- Use patch releases such as `v1.0.8` for compatibility-preserving fixes.
- Reserve minor releases such as `v1.1.0` for new CLI flags, config behavior, or workflow changes.
- Note upstream PRs and issues adopted in the changelog.

## Preflight

1. Confirm the working tree is clean except for intended release changes.
2. Fetch upstream:

   ```shell
   git fetch upstream
   ```

3. Run pure Go tests:

   ```shell
   go test ./config ./internal/beatport ./internal/validator
   ```

4. Run full tests with TagLib headers installed:

   ```shell
   go test ./...
   ```

5. Smoke test one track, release, playlist, and chart with a valid subscription.
6. Verify tags, cover handling, filenames, progress output, and error logs.

## Build Targets

The release should include:

- `beatportdl-windows-amd64.exe`
- `beatportdl-darwin-amd64`
- `beatportdl-darwin-arm64`
- `beatportdl-linux-amd64`
- `beatportdl-linux-arm64`

Set the required TagLib/zlib include and library paths before running the Makefile targets.

## v1.0.8 Candidate Notes

Include these highlights if present in the release:

- Maintained fork documentation and support policy.
- Upstream PR #100: playlist/chart `first_genre` uses the first listed track genre.
- Upstream PR #123: default M4A BPM tag mapping writes through `BPM_raw`.
- CI coverage for pure Go tests and a TagLib-enabled full test job.
