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
