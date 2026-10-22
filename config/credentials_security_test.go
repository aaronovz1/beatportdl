package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestCredentialDefaultConfigRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Username = "synthetic"
	cfg.Password = "synthetic"
	cfg.DownloadsDirectory = t.TempDir()
	file := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(file); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	// Parsing populates derived tag mappings when they were not supplied.
	cfg.TagMappings = got.TagMappings
	if !reflect.DeepEqual(cfg, *got) {
		t.Error("default config changed across Save/Parse")
	}
}

func TestCredentialSaveUsesPrivateReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode assertion; Windows DACL coverage lives in securefile")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(file)
	cfg := &AppConfig{Username: "synthetic", Password: "not-a-secret", DownloadsDirectory: dir}
	if err := cfg.Save(file); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0600 {
		t.Errorf("mode = %04o, want 0600", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Error("Save modified the original inode instead of replacing it")
	}
}

func TestCredentialSaveRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := (&AppConfig{Username: "synthetic", Password: "synthetic"}).Save(link); err == nil {
		t.Error("Save accepted symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Errorf("target changed: %q, %v", data, err)
	}
}

func TestCredentialParseRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("username: synthetic\npassword: synthetic\ndownloads_directory: .\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Parse(link); err == nil {
		t.Error("Parse accepted symlink")
	}
}

func TestCredentialSaveRoundTripsFalseZeroAndEmpty(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(file, []byte("username: synthetic\npassword: synthetic\ndownloads_directory: .\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ShowProgress = false
	cfg.FixTags = false
	cfg.TrackNumberPadding = 0
	cfg.ArtistsLimit = 0
	cfg.ArtistsShortForm = ""
	cfg.CoverSize = ""
	cfg.TrackFileTemplate = ""
	cfg.ReleaseDirectoryTemplate = ""
	cfg.PlaylistDirectoryTemplate = ""
	cfg.ChartDirectoryTemplate = ""
	cfg.LabelDirectoryTemplate = ""
	cfg.ArtistDirectoryTemplate = ""
	if err := cfg.Save(file); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShowProgress || got.FixTags || got.TrackNumberPadding != 0 || got.ArtistsLimit != 0 {
		t.Errorf("false/zero settings changed: show_progress=%v fix_tags=%v padding=%d artists_limit=%d", got.ShowProgress, got.FixTags, got.TrackNumberPadding, got.ArtistsLimit)
	}
	for name, value := range map[string]string{"cover": got.CoverSize, "short_form": got.ArtistsShortForm, "track": got.TrackFileTemplate, "release": got.ReleaseDirectoryTemplate, "playlist": got.PlaylistDirectoryTemplate, "chart": got.ChartDirectoryTemplate, "label": got.LabelDirectoryTemplate, "artist": got.ArtistDirectoryTemplate} {
		if value != "" {
			t.Errorf("empty %s setting changed to %q", name, value)
		}
	}
}
