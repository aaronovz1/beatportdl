# BeatportDL

Beatport downloader (FLAC, AAC)

_Requires an active [Beatport](https://stream.beatport.com/) streaming plan._

> This is the maintained fork at [aaronovz1/beatportdl](https://github.com/aaronovz1/beatportdl). It keeps compatibility with the original [unspok3n/beatportdl](https://github.com/unspok3n/beatportdl) project while carrying forward fixes, user support, and releases from this fork.

![Screenshot](/screenshots/main.png?raw=true "Screenshot")

## Support Policy

BeatportDL works only with active Beatport streaming subscriptions. This fork does not provide help bypassing subscriptions, territorial restrictions, account limits, or other platform access controls.

For bugs and feature requests, please use the issue templates in this fork and include your OS, BeatportDL version, subscription tier, configured quality, URL type, and sanitized logs. Remove usernames, passwords, tokens, cookies, and local private paths before posting logs.

Beatsource is no longer supported. Beatport is phasing out the standalone service and migrating accounts to Beatport; see the official [Beatsource migration FAQ](https://support.beatport.com/hc/en-us/articles/46607140394004-Beatsource-Migration-FAQ). Use the migrated URLs from your Beatport account. Old Beatsource URLs and `@beatsource` searches are rejected with migration guidance; URLs are not automatically rewritten because resource IDs may differ. Existing config and credential files are not migrated or deleted by this change.

## Label and Artist Filters

Label and artist URLs use the catalog tracks endpoint. Query filters such as `genre_name`, `publish_date`, and `order_by` are forwarded on every page. Label downloads include only matching tracks, not unrelated tracks from the same release. Genre names must be the full exact name (case-insensitive), for example `?genre_name=bass+house`.

With `sort_by_context` enabled, artist downloads use `Artists/<artist_directory_template>/` and label downloads use `Labels/<label_directory_template>/` beneath `downloads_directory`. Both put tracks directly in their context directory by default; enable `force_release_directories` to add release subdirectories. Separate `cover.jpg` files are kept only with release subdirectories for these downloads, avoiding conflicting release covers in one artist or label folder. Embedded cover tagging is unchanged.

Existing-file checks still use the target directory and embedded track identity; this change does not search other download directories or move existing files. Move existing artist/label context folders beneath their new `Artists`/`Labels` groups before rerunning. If they contain release subdirectories, keep `force_release_directories` enabled or move the audio files into the context directory to avoid downloading them again. Set `sort_by_context: false` (or `--sort-by-context=false`) to download directly into `downloads_directory` without groups or release subdirectories.

## Setup

1. [Download](https://github.com/aaronovz1/beatportdl/releases/) or [build](#building) BeatportDL.

   _Compiled binaries for Windows, macOS (amd64, arm64) and Linux (amd64, arm64) are available on the [Releases](https://github.com/aaronovz1/beatportdl/releases) page._ \
    _Don't forget to set the execute permission on unix systems, e.g., chmod +x beatportdl-darwin-arm64_

2. Run beatportdl (e.g. `./beatportdl-darwin-arm64`), then specify the:

   - Beatport username
   - Beatport password
   - Downloads directory
   - Audio quality

3. OPTIONAL: Customize a config file. Create a new config file by running:

```shell
./beatportdl
```

This will create a new `beatportdl-config.yml` file. You can put the following options and values into the config file:

---

| Option                        | Default Value                             | Type       | Description                                                                                                                                                                               |
| ----------------------------- | ----------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `username`                    |                                           | String     | Beatport username                                                                                                                                                                         |
| `password`                    |                                           | String     | Beatport password                                                                                                                                                                         |
| `quality`                     | lossless                                  | String     | Download quality _(medium-hls, medium, high, lossless)_                                                                                                                                   |
| `show_progress`               | true                                      | Boolean    | Enable progress bars                                                                                                                                                                      |
| `write_error_log`             | false                                     | Boolean    | Write errors to `beatportdl-err.log`                                                                                                                                                               |
| `max_download_workers`        | 15                                        | Integer    | Concurrent download jobs limit                                                                                                                                                            |
| `max_global_workers`          | 15                                        | Integer    | Concurrent global jobs limit                                                                                                                                                              |
| `downloads_directory`         |                                           | String     | Location for the downloads directory                                                                                                                                                      |
| `sort_by_context`             | false                                     | Boolean    | Create a directory for each release, playlist, chart, label, or artist                                                                                                                    |
| `sort_by_label`               | false                                     | Boolean    | Use label names as parent directories for releases (requires `sort_by_context`)                                                                                                           |
| `force_release_directories`   | false                                     | Boolean    | Create release directories inside chart, playlist, artist, and label folders (requires `sort_by_context`)                                                                                  |
| `track_exists`                | update                                    | String     | Behavior when track file already exists                                                                                                                                                   |
| `track_number_padding`        | 2                                         | Integer    | Track number padding for filenames and tag mappings (when using `track_number_with_padding` or `release_track_count_with_padding`)<br/> Set to 0 for dynamic padding based on track count |
| `cover_size`                  | 1400x1400                                 | String     | Cover art size for `keep_cover` and track metadata (if `fix_tags` is enabled) _[max: 1400x1400]_                                                                                          |
| `keep_cover`                  | false                                     | Boolean    | Download cover art file (cover.jpg) to the context directory (requires `sort_by_context`)                                                                                                 |
| `fix_tags`                    | true                                      | Boolean    | Enable tag writing capabilities                                                                                                                                                           |
| `tag_mappings`                | _Listed below_                            | String Map | Custom tag mappings                                                                                                                                                                       |
| `track_file_template`         | {number}. {artists} - {name} ({mix_name}) | String     | Track filename template                                                                                                                                                                   |
| `release_directory_template`  | [{catalog_number}] {artists} - {name}     | String     | Release directory template                                                                                                                                                                |
| `playlist_directory_template` | {name} [{created_date}]                   | String     | Playlist directory template                                                                                                                                                               |
| `chart_directory_template`    | {name} [{published_date}]                 | String     | Chart directory template                                                                                                                                                                  |
| `label_directory_template`    | {name} [{updated_date}]                   | String     | Label directory template                                                                                                                                                                  |
| `artist_directory_template`   | {name}                                    | String     | Artist directory template                                                                                                                                                                 |
| `whitespace_character`        |                                           | String     | Whitespace character for track filenames and release directories                                                                                                                          |
| `artists_limit`               | 3                                         | Integer    | Maximum number of artists allowed before replacing with `artists_short_form` (affects directories, filenames, and search results)                                                         |
| `artists_short_form`          | VA                                        | String     | Custom string to represent "Various Artists"                                                                                                                                              |
| `key_system`                  | standard-short                            | String     | Music key system used in filenames and tags                                                                                                                                               |
| `proxy`                       |                                           | String     | Proxy URL                                                                                                                                                                                 |

If the Beatport credentials are correct, you should also see the file `beatportdl-credentials.json` appear in the BeatportDL directory.
_If you accidentally entered an incorrect password and got an error, you can always manually edit the config file_

Config, credentials, and error logs must be regular files in a location that
other users cannot modify. Existing portable files are supported, but symlinks,
hardlinks, shared writable parents, and unsafe access-control lists are rejected.
Owned, broadly readable legacy files are replaced privately when loaded; their
contents are preserved. Keep these files outside shared/cloud music directories.
On Linux, the fallback locations are `$XDG_CONFIG_HOME/beatportdl` and
`$XDG_STATE_HOME/beatportdl` (or their standard home-directory defaults).

`whitespace_character` replaces spaces inside each filename or directory component. An empty value keeps spaces; safe replacements such as `_` and `-` are supported. Path separators, Windows-reserved filename characters and control characters are rejected in config files and CLI overrides. Define folder hierarchy with `/` in the directory template instead.

Download quality options for Beatport (availability depends on your active subscription tier and the track):

| Option       | Description                                                                                                  | Requires at least              | Notes                                                                   |
| ------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------ | ----------------------------------------------------------------------- |
| `medium-hls` | 128 kbps AAC through `/stream` endpoint (IMPORTANT: requires [ffmpeg](https://www.ffmpeg.org/download.html)) | Essential                     | Same as `medium` on Advanced but uses a slightly slower download method |
| `medium`     | 128 kbps AAC                                                                                                 | Advanced                      |                                                                         |
| `high`       | 256 kbps AAC                                                                                                 | Professional                  |                                                                         |
| `lossless`   | 44.1 kHz FLAC                                                                                                | Professional                  |                                                                         |

Available `track_exists` options:

- `error` Log error and skip
- `skip` Skip silently
- `overwrite` Re-download
- `update` Update tags

Existing-file checks use embedded track IDs or ISRCs in the destination directory, separately for each audio format. Matching files keep their existing names, including collision suffixes. The index is rebuilt for each download run and updated as tracks finish; deleted or replaced cached files are rechecked. Changed timestamps alone do not cause a re-download when the embedded identity still matches. Files without identity tags retain filename-based matching across runs; files in other directories are not searched.

API name escapes are decoded before naming and tagging. Existing identity-matched files keep their names even if an older version rendered an escape literally. Legacy files without identity tags may need renaming to match the corrected name before rerunning.

Available template keywords for filenames and directories (`*_template`):

- Track: `id`,`name`,`mix_name`,`slug`,`artists`,`remixers`,`number`,`length`,`key`,`bpm`,`genre`,`subgenre`,`genre_with_subgenre`,`subgenre_or_genre`,`isrc`,`label`
- Release: `id`,`name`,`slug`,`artists`,`remixers`,`date`,`year`,`track_count`,`bpm_range`,`catalog_number`,`upc`,`label`
- Playlist: `id`,`name`,`first_genre`,`track_count`,`bpm_range`,`length`,`created_date`,`updated_date`
- Chart: `id`,`name`,`slug`,`first_genre`,`track_count`,`creator`,`created_date`,`published_date`,`updated_date`

**Note:** The `first_genre` parameter for playlists and charts now uses the genre of the first track in the playlist/chart, rather than relying on metadata. This ensures more accurate genre-based folder organization.

- Artist: `id`, `name`, `slug`
- Label: `id`, `name`, `slug`, `created_date`, `updated_date`

Default `tag_mappings` config:

```yaml
tag_mappings:
  flac:
    track_name: "TITLE"
    track_artists: "ARTIST"
    track_number: "TRACKNUMBER"
    track_subgenre_or_genre: "GENRE"
    track_key: "KEY"
    track_bpm: "BPM"
    track_isrc: "ISRC"

    release_name: "ALBUM"
    release_artists: "ALBUMARTIST"
    release_date: "DATE"
    release_track_count: "TOTALTRACKS"
    release_catalog_number: "CATALOGNUMBER"
    release_label: "LABEL"
  m4a:
    track_name: "TITLE"
    track_artists: "ARTIST"
    track_number: "TRACKNUMBER"
    track_genre: "GENRE"
    track_key: "KEY"
    track_bpm: "BPM_raw"
    track_isrc: "ISRC"

    release_name: "ALBUM"
    release_artists: "ALBUMARTIST"
    release_date: "DATE"
    release_track_count: "TOTALTRACKS"
    release_catalog_number: "CATALOGNUMBER"
    release_label: "LABEL"
```

As you can see, each key here represents a predefined value from either a release or a track that you can use to customize what is written to which tags. When you add an entry in the mappings for any format (for e.g., `flac`), only the tags that you specify will be written.

All tags by default are converted to uppercase, but since some M4A players might not recognize it, you can write the tag in lowercase and add the `_raw` suffix to bypass the conversion. _(This applies to M4A tags only)_

For e.g., Traktor doesn't recognize the track key tag in uppercase, so you have to add:

```yaml
tag_mappings:
  m4a:
    track_key: "initialkey_raw"
```

Available `tag_mappings` keys: `track_id`,`track_url`,`track_name`,`track_artists`,`track_artists_limited`,`track_remixers`,`track_remixers_limited`,`track_number`,`track_number_with_padding`,`track_number_with_total`,`track_genre`,`track_subgenre`,`track_genre_with_subgenre`,`track_subgenre_or_genre`,`track_key`,`track_bpm`,`track_isrc`,`release_id`,`release_url`,`release_name`,`release_artists`,`release_artists_limited`,`release_remixers`,`release_remixers_limited`,`release_date`,`release_year`,`release_track_count`,`release_track_count_with_padding`,`release_catalog_number`,`release_upc`,`release_label`,`release_label_url`

Available `key_system` options:

| System           | Example           |
| ---------------- | ----------------- |
| `standard`       | Eb Minor, F Major |
| `standard-short` | Ebm, F            |
| `openkey`        | 7m, 12d           |
| `camelot`        | 2A, 7B            |

Proxy URL format example: `http://username:password@127.0.0.1:8080`

## Usage

During a download, the first `Ctrl+C` cancels queued work, active HTTP transfers and FFmpeg operations, then waits for workers to stop and clean their temporary files. A second shutdown signal forces an immediate exit and may leave temporary files behind. Completed tracks are kept.

Media requests wait up to 40 seconds for response headers and up to 30 minutes overall per request, allowing large audio downloads more time than catalog requests. Authentication recovery retries a rejected catalog request once after refreshing the token, then reports a persistent authorization error.

An `invalid API response` error means a catalog response was incomplete or malformed; it is not treated as an empty playlist or a valid resource. Valid empty results remain supported, and completed tracks are kept.

Playlist/chart updates share release metadata within each collection. With `track_exists: skip`, identity-matched files avoid extra metadata lookups, but BeatportDL still checks access and the server-selected audio format for each track. Worker limits apply before new goroutines are started, keeping queued work bounded on large collections.

Empty output directories are cleaned only after all active URL workers finish, so concurrent downloads can safely share a folder. Completed files, nonempty directories and the configured download root are preserved; temporary audio and cover files are still cleaned by their individual downloads.

Run BeatportDL and enter a Beatport URL or search query:

```shell
./beatportdl
Enter url or search query:
```

Search returns Beatport tracks and releases.

...or specify the URL using positional arguments:

```shell
./beatportdl https://www.beatport.com/track/strobe/1696999 https://www.beatport.com/track/move-for-me/591753
```

...or provide a text file with urls (separated by a newline)

```shell
./beatportdl file.txt file2.txt
```

URL types that are currently supported: **Tracks, Releases, Playlists, Charts, Labels, Artists**

### Command-Line Overrides

Every config option can be overridden for one run. Use the config option name with underscores replaced by hyphens: `downloads_directory` becomes `--downloads-directory`. Values are applied after loading the config file and before validation or login. Omitted flags keep their configured values; overrides do not modify the config file.

Put flags before URL or text-file arguments. Quote paths and templates containing spaces:

```shell
./beatportdl -q --downloads-directory "/Music/Moved Playlist" --sort-by-context=false --track-exists=skip https://www.beatport.com/library/playlists/123456
./beatportdl --quality=high --max-download-workers=4 --track-number-padding=0 tracks.txt
./beatportdl --playlist-directory-template="{first_genre}/{name}" --keep-cover tracks.txt
```

Boolean flags without a value enable the option. Use `--fix-tags=false` to disable it; the `=false` syntax is required. Zero integers and empty strings are explicit overrides, such as `--track-number-padding=0` or `--whitespace-character=`. Worker counts must be positive. Repeated flags use the last value.

`--tag-mappings` takes a quoted YAML or JSON map and replaces the configured map. Formats omitted from the replacement receive the usual default mappings:

```shell
./beatportdl --tag-mappings='{"m4a":{"track_bpm":"BPM_raw","track_name":"TITLE"}}' tracks.txt
```

Use `--help` to list flags without loading the config file or logging in. `-q` exits after downloads finish. Prefer keeping passwords in the config file because command-line passwords can appear in shell history and process listings. First-run config creation still uses the setup prompts.

## Development

Containerized CI checks with pinned builder inputs:

```shell
./scripts/docker-build.sh test
```

Pure Go checks that do not require TagLib headers:

```shell
go test ./config ./internal/beatport ./internal/validator
```

Full tests/build checks require TagLib headers and libraries:

```shell
go test ./...
```

If the full test command fails with `fatal error: 'taglib/tag_c.h' file not found`, install TagLib development headers or pass the correct `-I` and `-L` paths through the build environment described below.

## Building

The maintained fork now ships with a containerized builder so supported release builds do not depend on host-installed Zig, TagLib, or zlib.

The Docker wrapper requires Git and Python 3 on the host to prepare an isolated source snapshot.

Containerized release commands:

```shell
./scripts/docker-build.sh release-core
```

That builds:

- `beatportdl-linux-amd64`
- `beatportdl-linux-arm64`

To cross-build Windows `amd64` locally without MSYS2 or host libraries:

```shell
./scripts/docker-build.sh windows-amd64
```

This produces `bin/beatportdl-windows-amd64.exe`. Containerized dependency builds use Zig's archive tools as well as its compilers, so foreign-target library indexes do not depend on the host's binutils.

The builder pins the Go image and Dockerfile frontend by digest, verifies Zig
and zlib checksums, and checks TagLib's exact commit and pristine source tree.
This verifies input identities, not the maintainer's cryptographic signature.
Build scripts create a minimal source snapshot without local credentials or
`.env`, mount it read-only, and write only to a separate output directory.
Compilation and tests run without external networking or elevated capabilities.
Docker is one isolation layer, not a guarantee against malicious build tools.

GitHub Actions is now configured to validate release builds on every push and pull request with this split:

- Linux artifacts come from the pinned Docker builder on `ubuntu-latest`.
- macOS `amd64` artifacts come from a native `macos-15-intel` runner.
- macOS `arm64` artifacts come from a native `macos-15` runner.
- Windows `amd64` artifacts come from a native `windows-2022` runner with MSYS2 MinGW.

CI also cross-builds Windows with Docker and checks its DLL imports to guard the local build path. Official Windows releases still use the native Windows runner.

Tagged releases are published by `.github/workflows/release.yml` after tests and
platform builds for the exact release commit pass. Release jobs use pinned
actions and read-only build tokens; publication alone receives write authority.
Assets include `SHA256SUMS`, dependency inventories, and GitHub provenance
attestations. See [the release checklist](docs/RELEASE.md) for verification.

macOS builds still require an Apple SDK because of Apple licensing. If you have one locally, mount it through `MACOS_SDK_PATH`:

```shell
MACOS_SDK_PATH=/path/to/MacOSX.sdk ./scripts/docker-build.sh darwin-arm64
MACOS_SDK_PATH=/path/to/MacOSX.sdk ./scripts/docker-build.sh darwin-amd64
```

You can also use Make targets as wrappers:

```shell
make docker-test
make docker-release-core
```

Host-managed build prerequisites, if you choose not to use Docker:

- [Go](https://go.dev/dl/) 1.27.3
- [TagLib](https://github.com/taglib/taglib) 2.3.2
- [zlib](https://github.com/madler/zlib) 1.3.2
- [Zig C/C++ Toolchain](https://github.com/ziglang/zig) 0.16.0 for container cross-builds

BeatportDL uses [TagLib](https://taglib.org/) C bindings to handle audio metadata and therefore requires [CGO](https://go.dev/wiki/cgo)

Makefile is adapted for cross-compilation and uses [Zig toolchain](https://github.com/ziglang/zig)

To compile BeatportDL with Zig using Makefile, you must specify the paths to the C/C++ libraries folder and headers folder for the desired OS and architecture with `-L` (for libraries) and `-I` (for headers) flags using environment variables: `MACOS_ARM64_LIB_PATH`, `MACOS_AMD64_LIB_PATH`, `LINUX_AMD64_LIB_PATH`, `LINUX_ARM64_LIB_PATH`, `WINDOWS_AMD64_LIB_PATH`

One line example _(for unix and unix-like os)_

```shell
MACOS_ARM64_LIB_PATH="-L/usr/local/lib -I/usr/local/include" \
make darwin-arm64
```

You can also create an `.env` file in the project folder and specify all environment variables in it:

```
MACOS_ARM64_LIB_PATH=-L/libraries/for/macos-arm64 -I/headers/for/macos-arm64
MACOS_AMD64_LIB_PATH=-L/libraries/for/macos-amd64 -I/headers/for/macos-amd64
LINUX_AMD64_LIB_PATH=-L/libraries/for/linux-amd64 -I/headers/for/linux-amd64
LINUX_ARM64_LIB_PATH=-L/libraries/for/linux-arm64 -I/headers/for/linux-arm64
WINDOWS_AMD64_LIB_PATH=-L/libraries/for/windows-amd64 -I/headers/for/windows-amd64
```

See [CHANGELOG.md](CHANGELOG.md) for release notes and [docs/RELEASE.md](docs/RELEASE.md) for the release checklist used by this fork.
