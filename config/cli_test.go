package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unspok3n/beatportdl/internal/securefile"
)

func TestCommandLineOverridesConfigWithoutChangingFile(t *testing.T) {
	contents := []byte("username: file-user\npassword: file-pass\ndownloads_directory: /original\nquality: lossless\nsort_by_context: true\nfix_tags: true\ntrack_number_padding: 2\nwhitespace_character: _\nkeep_cover: true\n")
	file := filepath.Join(t.TempDir(), "config.yml")
	if err := securefile.Write(file, contents); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseCommandLine([]string{
		"--downloads-directory", "/moved playlist", "--quality=high",
		"--sort-by-context=false", "--fix-tags=false", "--track-number-padding=0",
		"--whitespace-character=", "-q", "tracks.txt", "https://www.beatport.com/track/example/123",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(file, opts.Apply)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DownloadsDirectory != "/moved playlist" || cfg.Quality != "high" || cfg.SortByContext || cfg.FixTags || cfg.TrackNumberPadding != 0 || cfg.WhitespaceCharacter != "" {
		t.Fatalf("explicit overrides were not applied: %+v", cfg)
	}
	if cfg.Username != "file-user" || cfg.Password != "file-pass" || !cfg.KeepCover || cfg.MaxDownloadWorkers != 15 {
		t.Fatal("omitted flags changed config-file values or defaults")
	}
	if !opts.Quit || !reflect.DeepEqual(opts.Args, []string{"tracks.txt", "https://www.beatport.com/track/example/123"}) {
		t.Fatalf("quit or positional arguments lost: %+v", opts)
	}
	actual, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(actual, contents) {
		t.Fatalf("config file was modified: %v", err)
	}
}

func TestCommandLineOverridesBeforeValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("PATH", "")
	if err := securefile.Write(file, []byte("key_system: invalid\nquality: medium-hls\n")); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseCommandLine([]string{"--username=user", "--password=pass", "--downloads-directory=/downloads", "--key-system=camelot", "--quality=high"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(file, opts.Apply)
	if err != nil {
		t.Fatalf("config was validated before overrides: %v", err)
	}
	if cfg.KeySystem != "camelot" {
		t.Fatalf("KeySystem = %q", cfg.KeySystem)
	}
}

func TestCommandLineAllConfigFields(t *testing.T) {
	opts, err := ParseCommandLine([]string{
		"--username=user", "--password=pass", "--quality=medium",
		"--write-error-log", "--show-progress", "--max-global-workers=2", "--max-download-workers=3",
		"--downloads-directory=/dest", "--sort-by-context", "--sort-by-label", "--force-release-directories",
		"--track-exists=skip", "--track-number-padding=4", "--release-directory-template=release",
		"--playlist-directory-template={first_genre}/{name}", "--chart-directory-template=chart",
		"--label-directory-template=label", "--artist-directory-template=artist", "--track-file-template=track",
		"--whitespace-character=_", "--artists-limit=5", "--artists-short-form=Various",
		"--key-system=openkey", "--cover-size=500x500", "--keep-cover", "--fix-tags",
		"--proxy=http://localhost:8080", `--tag-mappings={"flac":{"track_name":"TITLE"}}`,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &AppConfig{}
	opts.Apply(cfg)
	want := &AppConfig{
		Username: "user", Password: "pass", Quality: "medium", WriteErrorLog: true, ShowProgress: true,
		MaxGlobalWorkers: 2, MaxDownloadWorkers: 3, DownloadsDirectory: "/dest", SortByContext: true,
		SortByLabel: true, ForceReleaseDirectories: true, TrackExists: "skip", TrackNumberPadding: 4,
		ReleaseDirectoryTemplate: "release", PlaylistDirectoryTemplate: "{first_genre}/{name}",
		ChartDirectoryTemplate: "chart", LabelDirectoryTemplate: "label", ArtistDirectoryTemplate: "artist",
		TrackFileTemplate: "track", WhitespaceCharacter: "_", ArtistsLimit: 5, ArtistsShortForm: "Various",
		KeySystem: "openkey", CoverSize: "500x500", KeepCover: true, FixTags: true, Proxy: "http://localhost:8080",
		TagMappings: map[string]map[string]string{"flac": {"track_name": "TITLE"}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config flags wired incorrectly: got %+v, want %+v", cfg, want)
	}
}

func TestCommandLineRepeatedFlagsUseLastValue(t *testing.T) {
	opts, err := ParseCommandLine([]string{"--quality=medium", "--quality=high", "--keep-cover", "--keep-cover=false"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &AppConfig{KeepCover: true}
	opts.Apply(cfg)
	if cfg.Quality != "high" || cfg.KeepCover {
		t.Fatalf("last option did not win: %+v", cfg)
	}
}

func TestCommandLineConfigOverridesAreValidated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yml")
	if err := securefile.Write(file, []byte("username: user\npassword: pass\ndownloads_directory: /downloads\n")); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		"--max-download-workers=0", "--max-global-workers=-1", "--track-exists=invalid",
		"--key-system=invalid", "--track-number-padding=11", "--downloads-directory=",
		"--tag-mappings={mp3: {track_name: TITLE}}",
	} {
		t.Run(arg, func(t *testing.T) {
			opts, err := ParseCommandLine([]string{arg}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(file, opts.Apply); err == nil {
				t.Fatal("expected invalid override to fail validation")
			}
		})
	}
}

func TestCommandLineErrorsAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--unknown"}, {"--max-download-workers=no"}, {"--fix-tags=perhaps"},
		{"--quality"}, {"--tag-mappings=not-a-map"},
	} {
		t.Run(args[0], func(t *testing.T) {
			if _, err := ParseCommandLine(args, io.Discard); err == nil {
				t.Fatal("expected argument error")
			}
		})
	}
	var output bytes.Buffer
	if _, err := ParseCommandLine([]string{"--help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("downloads-directory")) {
		t.Fatal("help missing config flags")
	}
}

func TestTagMappingsRejectDuplicateAndTrailingDocuments(t *testing.T) {
	for _, value := range []string{
		"flac: {track_name: TITLE, track_name: ARTIST}",
		"flac: {track_name: TITLE}\n---\nm4a: {track_name: TITLE}",
	} {
		if _, err := ParseCommandLine([]string{"--tag-mappings=" + value}, io.Discard); err == nil {
			t.Errorf("accepted ambiguous mappings: %q", value)
		}
	}
}

func TestConfigLegacyBooleanAndRoundTripCompatibility(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yml")
	if err := securefile.Write(file, []byte("username: fixture\npassword: fixture\ndownloads_directory: /downloads\nkeep_cover: yes\nshow_progress: off\n")); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(file)
	if err != nil || !cfg.KeepCover || cfg.ShowProgress {
		t.Fatalf("legacy boolean values changed: %+v, %v", cfg, err)
	}
	if err := cfg.Save(file); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(file)
	if err != nil || !reflect.DeepEqual(cfg, got) {
		t.Fatalf("config round trip changed values: %v", err)
	}
}

func TestCommandLineTagMappingsReplaceConfigMapping(t *testing.T) {
	opts, err := ParseCommandLine([]string{`--tag-mappings={m4a: {track_bpm: BPM_raw}}`}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &AppConfig{TagMappings: map[string]map[string]string{"m4a": {"track_name": "TITLE"}}}
	opts.Apply(cfg)
	if !reflect.DeepEqual(cfg.TagMappings, map[string]map[string]string{"m4a": {"track_bpm": "BPM_raw"}}) {
		t.Fatalf("TagMappings = %v", cfg.TagMappings)
	}
}
