package main

import (
	"os"
	"path/filepath"
	"testing"
	"unspok3n/beatportdl/config"
)

func newTestApplication(trackExists string) *application {
	return &application{
		config: &config.AppConfig{
			TrackExists: trackExists,
		},
		activeFiles: make(map[string]int64),
	}
}

func TestReserveTrackFilePathUsesSuffixForActiveCollision(t *testing.T) {
	app := newTestApplication("update")
	dir := t.TempDir()

	first, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101)
	if err != nil {
		t.Fatalf("reserveTrackFilePath() first = %v", err)
	}

	second, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 202)
	if err != nil {
		t.Fatalf("reserveTrackFilePath() second = %v", err)
	}

	if first == second {
		t.Fatalf("expected unique reserved path, got %q for both tracks", first)
	}

	want := filepath.Join(dir, "01. Artist - Track (1).flac")
	if second != want {
		t.Fatalf("reserveTrackFilePath() = %q, want %q", second, want)
	}
}

func TestReserveTrackFilePathUsesSuffixForFinishedRunCollision(t *testing.T) {
	app := newTestApplication("update")
	dir := t.TempDir()

	first, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101)
	if err != nil {
		t.Fatalf("reserveTrackFilePath() first = %v", err)
	}

	if err := os.WriteFile(first, []byte("done"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", first, err)
	}

	second, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 202)
	if err != nil {
		t.Fatalf("reserveTrackFilePath() second = %v", err)
	}

	want := filepath.Join(dir, "01. Artist - Track (1).flac")
	if second != want {
		t.Fatalf("reserveTrackFilePath() = %q, want %q", second, want)
	}
}

func TestReserveTrackFilePathRespectsTrackExistsForPreexistingFile(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "01. Artist - Track.flac")
	if err := os.WriteFile(basePath, []byte("existing"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", basePath, err)
	}

	t.Run("skip", func(t *testing.T) {
		app := newTestApplication("skip")
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101)
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != "" {
			t.Fatalf("reserveTrackFilePath() = %q, want empty path", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		app := newTestApplication("update")
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101)
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != basePath {
			t.Fatalf("reserveTrackFilePath() = %q, want %q", got, basePath)
		}
	})

	t.Run("error", func(t *testing.T) {
		app := newTestApplication("error")
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101)
		if err != ErrTrackFileExists {
			t.Fatalf("reserveTrackFilePath() err = %v, want %v", err, ErrTrackFileExists)
		}
		if got != "" {
			t.Fatalf("reserveTrackFilePath() = %q, want empty path", got)
		}
	})
}

func TestCleanupCoverTempRemovesTemporaryCoverFile(t *testing.T) {
	dir := t.TempDir()
	tempCover := filepath.Join(dir, "3d57726c-ba93-4517-80801f4e")
	if err := os.WriteFile(tempCover, []byte("cover"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", tempCover, err)
	}

	cleanupCoverTemp(tempCover)

	if _, err := os.Stat(tempCover); !os.IsNotExist(err) {
		t.Fatalf("expected temporary cover file to be removed, stat err = %v", err)
	}
}

func TestCleanupCoverTempAfterHandleCoverFileKeepsCoverJPG(t *testing.T) {
	dir := t.TempDir()
	tempCover := filepath.Join(dir, "517f33bd-1b86-4b98-86...da35b7fb")
	if err := os.WriteFile(tempCover, []byte("cover"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", tempCover, err)
	}

	app := &application{
		config: &config.AppConfig{
			KeepCover:     true,
			SortByContext: true,
		},
	}

	if err := app.handleCoverFile(tempCover); err != nil {
		t.Fatalf("handleCoverFile(%s): %v", tempCover, err)
	}

	cleanupCoverTemp(tempCover)

	coverPath := filepath.Join(dir, "cover.jpg")
	if _, err := os.Stat(coverPath); err != nil {
		t.Fatalf("expected preserved cover file at %s: %v", coverPath, err)
	}
}
