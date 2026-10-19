package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
	"unspok3n/beatportdl/internal/taglib"
)

func writeIdentityAudio(t *testing.T, path, trackID, isrc string) {
	t.Helper()
	if err := os.WriteFile(path, audioFixture(t, "tone"+filepath.Ext(path)), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := taglib.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	file.SetProperty("BEATPORT_TRACK_ID", &trackID)
	file.SetProperty("ISRC", &isrc)
	if err := file.Save(); err != nil {
		t.Fatal(err)
	}
}

func identityTestApplication(policy string) *application {
	app := newTestApplication(policy)
	app.config.TagMappings = map[string]map[string]string{
		"flac": {"track_id": "BEATPORT_TRACK_ID", "track_isrc": "ISRC"},
		"m4a":  {"track_isrc": "ISRC"},
	}
	return app
}

func TestCompletedDownloadIsRecognizedAfterFilenameChange(t *testing.T) {
	for _, tc := range []struct {
		name         string
		fixTags      bool
		omitIdentity bool
	}{
		{"tags disabled", false, false},
		{"default mappings", true, false},
		{"mappings without identity", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audio := audioFixture(t, "tone.flac")
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = w.Write(audio)
			}))
			defer server.Close()
			dir := t.TempDir()
			app := directDownloadTestApplication(server.URL)
			app.config.TrackExists = "skip"
			app.config.FixTags = tc.fixTags
			app.config.CoverSize = config.DefaultCoverSize
			app.config.TagMappings = config.DefaultTagMappings
			if tc.omitIdentity {
				app.config.TagMappings = map[string]map[string]string{"flac": {"track_name": "TITLE"}}
			}
			track := &beatport.Track{ID: 101, ISRC: "GBABC2600001", Name: "Original"}
			if processed, err := app.handleTrack(nil, track, dir, nil); !processed || err != nil {
				t.Fatalf("initial download = %v, %v", processed, err)
			}
			track.Name = "Corrected"
			if processed, err := app.handleTrack(nil, track, dir, nil); processed || err != nil {
				t.Errorf("renamed track = %v, %v; want skipped", processed, err)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("audio requests = %d, want 1", got)
			}
			assertCatalogFiles(t, dir, "Original.flac")
		})
	}
}

func TestCompletedUntaggedHintIsInvalidatedWhenFileChanges(t *testing.T) {
	audio := audioFixture(t, "tone.flac")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(audio)
	}))
	defer server.Close()
	dir := t.TempDir()
	app := directDownloadTestApplication(server.URL)
	app.config.TrackExists = "skip"
	app.config.FixTags = true
	app.config.CoverSize = config.DefaultCoverSize
	app.config.TagMappings = map[string]map[string]string{"flac": {"track_name": "TITLE"}}
	track := &beatport.Track{ID: 101, Name: "Original"}
	if processed, err := app.handleTrack(nil, track, dir, nil); !processed || err != nil {
		t.Fatalf("initial download = %v, %v", processed, err)
	}
	path := filepath.Join(dir, "Original.flac")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "")
	if want := filepath.Join(dir, "Corrected.flac"); got != want || err != nil {
		t.Fatalf("changed file without persisted identity = %q, %v; want %q", got, err, want)
	}
}

func TestIdentityCacheDoesNotSkipDeletedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Old.flac")
	writeIdentityAudio(t, path, "101", "GBABC2600001")
	app := identityTestApplication("skip")
	if got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, ""); got != "" || err != nil {
		t.Fatalf("initial reservation = %q, %v; want skipped", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "GBABC2600001")
	if want := filepath.Join(dir, "Corrected.flac"); got != want || err != nil {
		t.Fatalf("reservation after deletion = %q, %v; want %q", got, err, want)
	}
}

func TestIdentityCacheRevalidatesReplacedFile(t *testing.T) {
	for _, replacement := range []string{"atomic same size and time", "edited tags", "edited tags restored timestamp", "directory"} {
		t.Run(replacement, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "Old.flac")
			writeIdentityAudio(t, path, "101", "GBABC2600001")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			app := identityTestApplication("skip")
			if got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, ""); got != "" || err != nil {
				t.Fatalf("initial reservation = %q, %v", got, err)
			}
			switch replacement {
			case "atomic same size and time":
				temp := filepath.Join(dir, "Replacement.flac")
				writeIdentityAudio(t, temp, "202", "GBABC2600002")
				if err := os.Chtimes(temp, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(temp, path); err != nil {
					t.Fatal(err)
				}
				updated, err := os.Stat(path)
				if err != nil || updated.Size() != info.Size() || !updated.ModTime().Equal(info.ModTime()) {
					t.Fatalf("replacement must retain size and mtime: %v", err)
				}
			case "edited tags":
				writeIdentityAudio(t, path, "202", "GBABC2600002")
			case "edited tags restored timestamp":
				writeIdentityAudio(t, path, "202", "GBABC2600002")
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				updated, err := os.Stat(path)
				if err != nil || !os.SameFile(info, updated) || updated.Size() != info.Size() || !updated.ModTime().Equal(info.ModTime()) {
					t.Fatalf("edit must retain inode, size and mtime: %v", err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "GBABC2600001")
			if want := filepath.Join(dir, "Corrected.flac"); got != want || err != nil {
				t.Fatalf("reservation after replacement = %q, %v; want %q", got, err, want)
			}
			if replacement != "directory" {
				got, err = app.reserveTrackFilePath(dir, "Other", ".flac", 202, "GBABC2600002")
				if got != "" || err != nil {
					t.Fatalf("replacement's real identity = %q, %v; want skipped", got, err)
				}
			}
		})
	}
}

func TestNoOpUpdateDoesNotInventTrackIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Original.flac")
	if err := os.WriteFile(path, audioFixture(t, "tone.flac"), 0600); err != nil {
		t.Fatal(err)
	}
	app := directDownloadTestApplication("unused-with-no-op-update")
	app.config.TrackExists = "update"
	app.config.FixTags = false
	track := &beatport.Track{ID: 101, Name: "Original"}
	if processed, err := app.handleTrack(nil, track, dir, nil); !processed || err != nil {
		t.Fatalf("no-op update = %v, %v", processed, err)
	}
	app.config.TrackExists = "skip"
	got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "")
	if want := filepath.Join(dir, "Corrected.flac"); got != want || err != nil {
		t.Fatalf("unverified file claimed requested identity: %q, %v; want %q", got, err, want)
	}
}

func TestDecodedJSONNameReusesExistingLegacyFilename(t *testing.T) {
	for _, extension := range []string{".flac", ".m4a"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			legacyName := "Caf\uff3cu00e9" + extension
			writeIdentityAudio(t, filepath.Join(dir, legacyName), "101", "GBABC2600001")
			var track beatport.Track
			if err := json.Unmarshal([]byte(`{"id":101,"name":"Caf\u00e9","isrc":"GBABC2600001"}`), &track); err != nil {
				t.Fatal(err)
			}
			if track.Name != "Caf\u00e9" {
				t.Fatalf("track name = %q, want decoded name", track.Name)
			}
			app := identityTestApplication("skip")
			app.config.Quality = "lossless"
			app.config.TrackFileTemplate = "{name}"
			app.config.FixTags = true
			app.downloadTrack = func(*beatport.Beatport, int64, string) (*beatport.TrackDownload, error) {
				quality := ".flac"
				if extension == ".m4a" {
					quality = ".256k.aac.mp4"
				}
				return &beatport.TrackDownload{Location: "unused-for-identity-match", StreamQuality: quality}, nil
			}
			cover := &trackCover{download: func() (string, error) {
				t.Error("skipped legacy track requested a cover")
				return "", errors.New("unexpected cover request")
			}}
			if processed, err := app.handleTrack(nil, &track, dir, cover); processed || err != nil {
				t.Fatalf("legacy track = %v, %v; want skipped", processed, err)
			}
			assertCatalogFiles(t, dir, legacyName)
		})
	}
}

func TestActiveCollisionReusesExistingSuffixedTrack(t *testing.T) {
	for _, policy := range []string{"skip", "update", "overwrite", "error"} {
		t.Run(policy, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "Shared.flac")
			suffix := filepath.Join(dir, "Shared (1).flac")
			writeIdentityAudio(t, base, "101", "GBABC2600001")
			writeIdentityAudio(t, suffix, "202", "GBABC2600002")
			app := identityTestApplication(policy)
			// A is already active when B's identical rendered filename is processed.
			app.activeFiles[base] = 101
			got, err := app.reserveTrackFilePath(dir, "Shared", ".flac", 202, "GBABC2600002")
			want := suffix
			var wantErr error
			if policy == "skip" || policy == "error" {
				want = ""
			}
			if policy == "error" {
				wantErr = ErrTrackFileExists
			}
			if got != want || !errors.Is(err, wantErr) {
				t.Fatalf("reservation = %q, %v; want %q, %v", got, err, want, wantErr)
			}
			if app.activeFiles[base] != 101 {
				t.Fatal("reservation changed the first track's ownership")
			}
			assertCatalogFiles(t, dir, "Shared.flac", "Shared (1).flac")
		})
	}
}

func TestIdentityMatchDoesNotStealActivePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Existing.flac")
	writeIdentityAudio(t, path, "101", "GBABC2600001")
	app := identityTestApplication("update")
	app.activeFiles[path] = 202
	got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "GBABC2600001")
	if want := filepath.Join(dir, "Corrected.flac"); got != want || err != nil {
		t.Fatalf("reservation = %q, %v; want %q", got, err, want)
	}
	if app.activeFiles[path] != 202 {
		t.Fatal("identity lookup stole another active track's path")
	}
}

func TestIdentityLookupsDoNotRescanDirectory(t *testing.T) {
	dir := t.TempDir()
	const count = 12
	for i := 1; i <= count; i++ {
		writeIdentityAudio(t, filepath.Join(dir, fmt.Sprintf("Track %02d.flac", i)), strconv.Itoa(i), "")
	}
	app := identityTestApplication("skip")
	reader := identityTestApplication("skip")
	reads := 0
	app.readTrackFileIdentity = func(path string) (trackFileIdentity, error) {
		reads++
		return reader.readExistingTrackFileIdentity(path)
	}
	for run := 0; run < 3; run++ {
		for i := 1; i <= count; i++ {
			got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", int64(i), "")
			if got != "" || err != nil {
				t.Fatalf("lookup %d = %q, %v", i, got, err)
			}
		}
	}
	if reads > 4*count {
		t.Fatalf("metadata reads = %d, want at most one directory scan plus one read per match (%d)", reads, 4*count)
	}
}

func TestNewRunRescansAddedFilesAndDropsReservations(t *testing.T) {
	dir := t.TempDir()
	app := identityTestApplication("skip")
	if _, err := app.reserveTrackFilePath(dir, "Reserved", ".flac", 101, ""); err != nil {
		t.Fatal(err)
	}
	writeIdentityAudio(t, filepath.Join(dir, "Imported.flac"), "101", "GBABC2600001")
	app.resetTrackFileState()
	if got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, ""); got != "" || err != nil {
		t.Fatalf("imported track on next run = %q, %v; want skipped", got, err)
	}
	got, err := app.reserveTrackFilePath(dir, "Reserved", ".flac", 202, "")
	if want := filepath.Join(dir, "Reserved.flac"); got != want || err != nil {
		t.Fatalf("old reservation on next run = %q, %v; want %q", got, err, want)
	}
}

func TestModifiedFileWithSameIdentityIsStillSkipped(t *testing.T) {
	for _, extension := range []string{".flac", ".m4a"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "Existing"+extension)
			writeIdentityAudio(t, path, "101", "GBABC2600001")
			app := identityTestApplication("skip")
			if got, err := app.reserveTrackFilePath(dir, "Corrected", extension, 101, "GBABC2600001"); got != "" || err != nil {
				t.Fatalf("initial lookup = %q, %v; want skipped", got, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			writeIdentityAudio(t, path, "101", "GBABC2600001")
			changed := info.ModTime().Add(2 * time.Second)
			if err := os.Chtimes(path, changed, changed); err != nil {
				t.Fatal(err)
			}
			if got, err := app.reserveTrackFilePath(dir, "Corrected", extension, 101, "GBABC2600001"); got != "" || err != nil {
				t.Fatalf("modified matching file = %q, %v; want skipped", got, err)
			}
		})
	}
}

func TestIdentityLookupKeepsAudioFormatsSeparate(t *testing.T) {
	dir := t.TempDir()
	writeIdentityAudio(t, filepath.Join(dir, "Existing.flac"), "101", "GBABC2600001")
	app := identityTestApplication("skip")
	if got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "GBABC2600001"); got != "" || err != nil {
		t.Fatalf("FLAC lookup = %q, %v; want skipped", got, err)
	}
	got, err := app.reserveTrackFilePath(dir, "Corrected", ".m4a", 101, "GBABC2600001")
	if want := filepath.Join(dir, "Corrected.m4a"); got != want || err != nil {
		t.Fatalf("M4A lookup = %q, %v; want %q", got, err, want)
	}
}

func TestIdentityLookupFallsBackWhenOneMatchingFileIsDeleted(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "One.flac")
	second := filepath.Join(dir, "Two.flac")
	writeIdentityAudio(t, first, "101", "GBABC2600001")
	writeIdentityAudio(t, second, "101", "GBABC2600001")
	app := identityTestApplication("update")
	path, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, "")
	if err != nil || (path != first && path != second) {
		t.Fatalf("initial reservation = %q, %v", path, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	want := first
	if path == first {
		want = second
	}
	if got, err := app.reserveTrackFilePath(dir, "Corrected", ".flac", 101, ""); got != want || err != nil {
		t.Fatalf("remaining matching file = %q, %v; want %q", got, err, want)
	}
}

func TestConcurrentReservationsReuseExistingCollisionFiles(t *testing.T) {
	dir := t.TempDir()
	const count = 12
	paths := make([]string, count)
	for i := range paths {
		name := "Shared.flac"
		if i != 0 {
			name = fmt.Sprintf("Shared (%d).flac", i)
		}
		paths[i] = filepath.Join(dir, name)
		writeIdentityAudio(t, paths[i], strconv.Itoa(i+1), "")
	}
	app := identityTestApplication("update")
	var workers sync.WaitGroup
	for i := range paths {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			got, err := app.reserveTrackFilePath(dir, "Shared", ".flac", int64(i+1), "")
			if got != paths[i] || err != nil {
				t.Errorf("track %d = %q, %v; want %q", i+1, got, err, paths[i])
			}
		}(i)
	}
	workers.Wait()
}
