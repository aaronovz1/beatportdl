package main

import (
	"os"
	"path/filepath"
	"testing"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
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

	first, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101, "")
	if err != nil {
		t.Fatalf("reserveTrackFilePath() first = %v", err)
	}

	second, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 202, "")
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

	first, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101, "")
	if err != nil {
		t.Fatalf("reserveTrackFilePath() first = %v", err)
	}

	if err := os.WriteFile(first, []byte("done"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", first, err)
	}

	second, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 202, "")
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
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101, "")
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != "" {
			t.Fatalf("reserveTrackFilePath() = %q, want empty path", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		app := newTestApplication("update")
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101, "")
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != basePath {
			t.Fatalf("reserveTrackFilePath() = %q, want %q", got, basePath)
		}
	})

	t.Run("error", func(t *testing.T) {
		app := newTestApplication("error")
		got, err := app.reserveTrackFilePath(dir, "01. Artist - Track", ".flac", 101, "")
		if err != ErrTrackFileExists {
			t.Fatalf("reserveTrackFilePath() err = %v, want %v", err, ErrTrackFileExists)
		}
		if got != "" {
			t.Fatalf("reserveTrackFilePath() = %q, want empty path", got)
		}
	})
}

func TestReserveTrackFilePathMatchesExistingFileByTrackIdentity(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "Artist One, Artist Two, Antagonite - Example Track (Extended Mix).flac")
	if err := os.WriteFile(existingPath, []byte("existing"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", existingPath, err)
	}

	t.Run("skip", func(t *testing.T) {
		app := newTestApplication("skip")
		app.readTrackFileIdentity = func(path string) (trackFileIdentity, error) {
			if path == existingPath {
				return trackFileIdentity{TrackID: "12345", ISRC: "GBABC2600001"}, nil
			}
			return trackFileIdentity{}, nil
		}

		got, err := app.reserveTrackFilePath(dir, "Artist One, Artist Two, Antâgonite - Example Track (Extended Mix)", ".flac", 12345, "GBABC2600001")
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != "" {
			t.Fatalf("reserveTrackFilePath() = %q, want empty path", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		app := newTestApplication("update")
		app.readTrackFileIdentity = func(path string) (trackFileIdentity, error) {
			if path == existingPath {
				return trackFileIdentity{TrackID: "12345", ISRC: "GBABC2600001"}, nil
			}
			return trackFileIdentity{}, nil
		}

		got, err := app.reserveTrackFilePath(dir, "Artist One, Artist Two, Antâgonite - Example Track (Extended Mix)", ".flac", 12345, "GBABC2600001")
		if err != nil {
			t.Fatalf("reserveTrackFilePath() = %v", err)
		}
		if got != existingPath {
			t.Fatalf("reserveTrackFilePath() = %q, want %q", got, existingPath)
		}
	})
}

func TestHandleTrackDoesNotRequestCoverWhenTrackIsSkipped(t *testing.T) {
	dir := t.TempDir()
	app := newTestApplication("skip")
	app.config.Quality = "lossless"
	app.config.FixTags = true
	app.config.CoverSize = "500x500"
	app.config.TrackFileTemplate = "{number}. {artists} - {name} ({mix_name})"
	app.config.ArtistsLimit = 3
	app.config.ArtistsShortForm = "VA"
	app.config.TrackNumberPadding = 2
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{
			Location:      "unused-when-track-is-skipped",
			StreamQuality: ".flac",
		}, nil
	}

	track := &beatport.Track{
		ID:      12345,
		Name:    beatport.SanitizedString("Existing"),
		MixName: beatport.SanitizedString("Original Mix"),
		Number:  1,
		Artists: beatport.Artists{
			{Name: "Artist"},
		},
		Release: beatport.Release{
			TrackCount: 1,
		},
	}

	existingPath := filepath.Join(dir, "01. Artist - Existing (Original Mix).flac")
	if err := os.WriteFile(existingPath, []byte("existing"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", existingPath, err)
	}

	cover := &trackCover{
		download: func() (string, error) {
			t.Fatal("cover should not be requested when the track is skipped")
			return "", nil
		},
	}

	processed, err := app.handleTrack(nil, track, dir, cover)
	if err != nil {
		t.Fatalf("handleTrack() = %v", err)
	}
	if processed {
		t.Fatal("handleTrack() processed skipped track")
	}
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
