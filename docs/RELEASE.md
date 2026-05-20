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

5. Run containerized tests:

   ```shell
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
   go test ./config ./internal/beatport ./internal/validator
   go test ./...
   ```

9. Smoke test one track, release, playlist, and chart with a valid subscription.
10. Verify tags, cover handling, filenames, progress output, and error logs.
11. Push the release tag and let `.github/workflows/release.yml` publish the GitHub release assets and `SHA256SUMS` after the runner jobs pass.
12. Ensure `CHANGELOG.md` contains the notes for the base version (`vX.Y.Z`), since the workflow publishes release notes from that section for both stable tags and `-rc` tags.

## Build Targets

The release should include:

- `beatportdl-darwin-amd64`
- `beatportdl-darwin-arm64`
- `beatportdl-linux-amd64`
- `beatportdl-linux-arm64`
- `beatportdl-windows-amd64.exe`

Linux targets are expected to come from the pinned Docker builder. macOS targets are expected to come from native GitHub macOS runners. Windows targets are expected to come from a native GitHub Windows runner with MSYS2 MinGW.

## v1.0.8 Candidate Notes

Include these highlights if present in the release:

- Maintained fork documentation and support policy.
- Upstream PR #100: playlist/chart `first_genre` uses the first listed track genre.
- Upstream PR #123: default M4A BPM tag mapping writes through `BPM_raw`.
- Dockerized Linux CI plus native macOS and Windows release publishing through GitHub Actions.
