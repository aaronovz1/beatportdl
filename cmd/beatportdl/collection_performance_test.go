package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
)

// Only the authenticated HTTP boundary is replaced: catalog parsing, scheduling,
// filename reservations and filesystem identity checks run through production code.
type collectionFixture struct {
	mu            sync.Mutex
	requests      map[string]int
	tracks        []beatport.Track
	release       beatport.Release
	quality       string
	status        int
	releaseStatus int
	pageSize      int
	trackStatus   func(int64) int
	chartImage    beatport.Image
}

func newCollectionFixture(t testing.TB, count int) (*application, *beatport.Beatport, *collectionFixture) {
	t.Helper()
	fixture := &collectionFixture{
		requests: make(map[string]int), quality: ".flac", status: http.StatusOK,
		release: beatport.Release{ID: 9, Name: "Release", TrackCount: count},
	}
	for i := 1; i <= count; i++ {
		fixture.tracks = append(fixture.tracks, beatport.Track{
			ID: int64(i), Name: beatport.SanitizedString(fmt.Sprintf("Track %d", i)),
			Number: i, Release: fixture.release,
		})
	}
	cachePath := filepath.Join(t.TempDir(), "synthetic-token.json")
	hash := fnv.New64a()
	_, _ = hash.Write([]byte("synthetic:synthetic"))
	token, err := json.Marshal(map[string]interface{}{
		"access_token": "synthetic-token", "login_id": fmt.Sprintf("%x", hash.Sum(nil)),
		"issued_at": time.Now().Unix(), "expires_in": 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, token, 0600); err != nil {
		t.Fatal(err)
	}
	auth := beatport.NewAuth("synthetic", "synthetic", cachePath)
	if err := auth.LoadCache(); err != nil {
		t.Fatal(err)
	}
	bp := beatport.New("", auth)
	clientField := reflect.ValueOf(bp).Elem().FieldByName("client")
	client := reflect.NewAt(clientField.Type(), unsafe.Pointer(clientField.UnsafeAddr())).Elem().Interface().(*http.Client)
	client.Transport = searchSelectionTransport(fixture.roundTrip)
	app := newTestApplication("skip")
	app.ctx = context.Background()
	app.logWriter = io.Discard
	app.downloadSem = make(chan struct{}, 15)
	app.config.DownloadsDirectory = t.TempDir()
	app.config.Quality = "lossless"
	app.config.CoverSize = config.DefaultCoverSize
	app.config.TrackFileTemplate = "{number}. {name}"
	app.config.ReleaseDirectoryTemplate = "{name}"
	app.config.PlaylistDirectoryTemplate = "{name}"
	app.config.ChartDirectoryTemplate = "{name}"
	app.config.TagMappings = config.DefaultTagMappings
	return app, bp, fixture
}

func (f *collectionFixture) roundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests[req.URL.Path]++
	f.mu.Unlock()
	status := http.StatusOK
	var data any
	switch {
	case req.URL.Path == "/v4/catalog/playlists/42/":
		data = beatport.Playlist{ID: 42, Name: "Collection"}
	case req.URL.Path == "/v4/catalog/charts/42/":
		data = beatport.Chart{ID: 42, Name: "Collection", Image: f.chartImage}
	case req.URL.Path == "/v4/catalog/playlists/42/tracks/" || req.URL.Path == "/v4/catalog/charts/42/tracks/":
		tracks := f.tracks
		var next *string
		if f.pageSize > 0 {
			page, _ := strconv.Atoi(req.URL.Query().Get("page"))
			start := (page - 1) * f.pageSize
			end := min(start+f.pageSize, len(tracks))
			if end < len(tracks) {
				url := fmt.Sprintf("?page=%d", page+1)
				next = &url
			}
			tracks = tracks[start:end]
		}
		if req.URL.Path == "/v4/catalog/playlists/42/tracks/" {
			items := make([]beatport.PlaylistItem, 0, len(tracks))
			for _, track := range tracks {
				items = append(items, beatport.PlaylistItem{Track: track})
			}
			data = beatport.Paginated[beatport.PlaylistItem]{Results: items, Next: next}
		} else {
			data = beatport.Paginated[beatport.Track]{Results: tracks, Next: next}
		}
	case strings.HasSuffix(req.URL.Path, "/download/"):
		status = f.status
		data = beatport.TrackDownload{StreamQuality: f.quality, Location: "https://fixture.invalid/audio"}
	case strings.HasSuffix(req.URL.Path, "/stream/"):
		status = f.status
		data = beatport.TrackStream{Url: "https://fixture.invalid/hls"}
	case req.URL.Path == "/v4/catalog/releases/9/":
		data = f.release
		if f.releaseStatus != 0 {
			status = f.releaseStatus
		}
	case strings.HasPrefix(req.URL.Path, "/v4/catalog/tracks/"):
		var id int
		if _, err := fmt.Sscanf(req.URL.Path, "/v4/catalog/tracks/%d/", &id); err != nil || id < 1 || id > len(f.tracks) {
			return nil, fmt.Errorf("unexpected track request: %s", req.URL)
		}
		track := f.tracks[id-1]
		track.Number = 10 + id // The full-track endpoint, not list order, owns numbering.
		data = track
		if f.trackStatus != nil {
			status = f.trackStatus(int64(id))
		}
	default:
		return nil, fmt.Errorf("unexpected fixture request: %s", req.URL)
	}
	if status != http.StatusOK {
		data = map[string]string{"detail": "subscription required"}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
}

func (f *collectionFixture) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}

func (f *collectionFixture) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, count := range f.requests {
		total += count
	}
	return total
}

func runCollection(app *application, bp *beatport.Beatport, kind string) {
	link := &beatport.Link{ID: 42, Original: "https://fixture.invalid/collection/42"}
	if kind == "playlist" {
		app.handlePlaylistLink(bp, link)
	} else {
		app.handleChartLink(bp, link)
	}
}

func seedCollectionIdentities(t testing.TB, app *application, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		path := filepath.Join(app.config.DownloadsDirectory, fmt.Sprintf("Old Name %d.flac", i))
		if err := os.WriteFile(path, []byte("existing audio"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	app.readTrackFileIdentity = func(path string) (trackFileIdentity, error) {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "Old Name "), ".flac")
		return trackFileIdentity{TrackID: id}, nil
	}
}

func TestCollectionNoOpSkipAvoidsRedundantMetadataRequests(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1000)
			seedCollectionIdentities(t, app, 1000)
			runCollection(app, bp, kind)
			if got := fixture.total(); got != 1002 {
				t.Fatalf("no-op requests = %d, want 1002 (collection + page + per-track access/format checks)", got)
			}
			if got := fixture.count("/v4/catalog/releases/9/"); got != 0 {
				t.Fatalf("release requests for skipped identities = %d, want 0", got)
			}
			entries, err := os.ReadDir(app.config.DownloadsDirectory)
			if err != nil || len(entries) != 1000 {
				t.Fatalf("existing files changed: count=%d err=%v", len(entries), err)
			}
		})
	}
}

func TestCollectionsShareReleaseMetadataAcrossPagesAndResetBetweenRuns(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 6)
			fixture.pageSize = 2
			app.config.TrackExists = "update"
			for i := 1; i <= 6; i++ {
				path := filepath.Join(app.config.DownloadsDirectory, fmt.Sprintf("%d. Track %d.flac", 10+i, i))
				if err := os.WriteFile(path, []byte("unchanged audio"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for run := 1; run <= 2; run++ {
				app.resetTrackFileState()
				runCollection(app, bp, kind)
				if got := fixture.count("/v4/catalog/releases/9/"); got != run {
					t.Fatalf("release requests after run %d = %d, want %d", run, got, run)
				}
			}
			assertCatalogFiles(t, app.config.DownloadsDirectory,
				"11. Track 1.flac", "12. Track 2.flac", "13. Track 3.flac", "14. Track 4.flac", "15. Track 5.flac", "16. Track 6.flac")
		})
	}
}

func TestCollectionEarlySkipUsesServerFormatAndStillChecksAccess(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		for _, mode := range []string{"matching format", "fallback format", "denied access", "HLS"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				app, bp, fixture := newCollectionFixture(t, 1)
				app.config.FixTags = true
				app.config.CoverSize = "500x500"
				fixture.release.Image = beatport.Image{DynamicURI: "https://fixture.invalid/cover"}
				ext := ".flac"
				if mode == "fallback format" || mode == "HLS" {
					ext = ".m4a"
					fixture.quality = ".256k.aac.mp4"
				}
				if mode == "denied access" {
					fixture.status = http.StatusForbidden
				}
				var logs bytes.Buffer
				app.logWriter = &logs
				if mode == "HLS" {
					app.config.Quality = "medium-hls"
				}
				path := filepath.Join(app.config.DownloadsDirectory, "Old Name"+ext)
				writeIdentityAudio(t, path, "1", "ISRC1")
				fixture.tracks[0].ISRC = "ISRC1"
				before, _ := os.ReadFile(path)
				var mediaRequests int
				app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
					mediaRequests++
					return nil, fmt.Errorf("unexpected audio or cover request: %s", req.URL)
				})}
				runCollection(app, bp, kind)
				endpoint := "/v4/catalog/tracks/1/download/"
				if mode == "HLS" {
					endpoint = "/v4/catalog/tracks/1/stream/"
				}
				if got := fixture.count(endpoint); got != 1 {
					t.Fatalf("access/format requests = %d, want 1", got)
				}
				if fixture.count("/v4/catalog/tracks/1/") != 0 || fixture.count("/v4/catalog/releases/9/") != 0 || mediaRequests != 0 {
					t.Fatalf("skipped/denied track fetched extra metadata or media: %+v media=%d", fixture.requests, mediaRequests)
				}
				if mode == "denied access" && !strings.Contains(logs.String(), "subscription required") {
					t.Fatalf("access error was hidden: %q", logs.String())
				}
				after, _ := os.ReadFile(path)
				if string(after) != string(before) {
					t.Fatal("existing file was modified")
				}
				assertCatalogFiles(t, app.config.DownloadsDirectory, "Old Name"+ext)
			})
		}
	}
}

func TestCollectionFormatMismatchDownloadsWithoutDuplicateLookup(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1)
			writeIdentityAudio(t, filepath.Join(app.config.DownloadsDirectory, "Old Name.m4a"), "1", "ISRC1")
			fixture.tracks[0].ISRC = "ISRC1"
			var mediaRequests int
			audio := audioFixture(t, "tone.flac")
			app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
				mediaRequests++
				return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(audio)), Body: io.NopCloser(strings.NewReader(string(audio)))}, nil
			})}
			runCollection(app, bp, kind)
			if got := fixture.count("/v4/catalog/tracks/1/download/"); got != 1 {
				t.Fatalf("download URL requests = %d, want 1", got)
			}
			if mediaRequests != 1 {
				t.Fatalf("audio requests = %d, want 1 for the missing FLAC", mediaRequests)
			}
			assertCatalogFiles(t, app.config.DownloadsDirectory, "Old Name.m4a", "11. Track 1.flac")
		})
	}
}

func TestCollectionSkipOnlyChecksTheSelectedReleaseDirectory(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1)
			app.config.SortByContext = true
			app.config.ForceReleaseDirectories = true
			dir := filepath.Join(app.config.DownloadsDirectory, "Collection", "Release")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			writeIdentityAudio(t, filepath.Join(dir, "Old Name.flac"), "1", "ISRC1")
			fixture.tracks[0].ISRC = "ISRC1"
			runCollection(app, bp, kind)
			if fixture.count("/v4/catalog/releases/9/") != 1 || fixture.count("/v4/catalog/tracks/1/") != 0 {
				t.Fatalf("nested skip requests = %+v; want one release, no full track", fixture.requests)
			}
			assertCatalogFiles(t, dir, "Old Name.flac")
		})
	}
}

func TestCollectionUntaggedFilenameSkipStillUsesFullTrackNumber(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1)
			path := filepath.Join(app.config.DownloadsDirectory, "11. Track 1.flac")
			if err := os.WriteFile(path, []byte("untagged existing file"), 0600); err != nil {
				t.Fatal(err)
			}
			runCollection(app, bp, kind)
			if fixture.count("/v4/catalog/tracks/1/") != 1 || fixture.count("/v4/catalog/releases/9/") != 1 || fixture.count("/v4/catalog/tracks/1/download/") != 1 {
				t.Fatalf("filename fallback requests = %+v", fixture.requests)
			}
			assertFileContents(t, path, "untagged existing file")
			assertCatalogFiles(t, app.config.DownloadsDirectory, "11. Track 1.flac")
		})
	}
}

func TestCollectionDoesNotSkipIdentityOutsideReleaseDirectory(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1)
			app.config.SortByContext = true
			app.config.ForceReleaseDirectories = true
			dir := filepath.Join(app.config.DownloadsDirectory, "Collection")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			writeIdentityAudio(t, filepath.Join(dir, "Old Name.flac"), "1", "ISRC1")
			fixture.tracks[0].ISRC = "ISRC1"
			audio := audioFixture(t, "tone.flac")
			app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(audio)), Body: io.NopCloser(bytes.NewReader(audio))}, nil
			})}
			runCollection(app, bp, kind)
			assertCatalogFiles(t, filepath.Join(dir, "Release"), "11. Track 1.flac")
			assertCatalogFiles(t, dir, "Old Name.flac", "Release")
		})
	}
}

func TestCollectionFirstGenreDirectoriesStillUseFirstTrack(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 1)
			app.config.SortByContext = true
			app.config.PlaylistDirectoryTemplate = "{first_genre}/{name}"
			app.config.ChartDirectoryTemplate = "{first_genre}/{name}"
			fixture.tracks[0].Genre = beatport.Genre{Name: "House"}
			dir := filepath.Join(app.config.DownloadsDirectory, "House", "Collection")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			writeIdentityAudio(t, filepath.Join(dir, "Old Name.flac"), "1", "ISRC1")
			fixture.tracks[0].ISRC = "ISRC1"
			runCollection(app, bp, kind)
			assertCatalogFiles(t, dir, "Old Name.flac")
			if fixture.count("/v4/catalog/tracks/1/") != 1 || fixture.count("/v4/catalog/releases/9/") != 0 {
				t.Fatalf("first genre requests = %+v; want one genre lookup and no release lookup", fixture.requests)
			}
		})
	}
}

func TestCollectionReleaseFailureIsSharedButRetriedOnNextRun(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 4)
			app.config.TrackExists = "update"
			fixture.releaseStatus = http.StatusForbidden
			runCollection(app, bp, kind)
			if got := fixture.count("/v4/catalog/releases/9/"); got != 1 {
				t.Fatalf("failed release requests = %d, want 1", got)
			}
			fixture.releaseStatus = http.StatusOK
			for i := 1; i <= 4; i++ {
				path := filepath.Join(app.config.DownloadsDirectory, fmt.Sprintf("%d. Track %d.flac", 10+i, i))
				if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			app.resetTrackFileState()
			runCollection(app, bp, kind)
			if got := fixture.count("/v4/catalog/releases/9/"); got != 2 {
				t.Fatalf("release requests after recovery = %d, want 2", got)
			}
			if fixture.count("/v4/catalog/tracks/1/") != 1 {
				t.Fatal("successful next run retained the old release error")
			}
		})
	}
}

type collectionLogFunc func([]byte) (int, error)

func (write collectionLogFunc) Write(data []byte) (int, error) { return write(data) }

func TestCollectionMetadataFailureDoesNotRemoveActiveReleaseDirectory(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, bp, fixture := newCollectionFixture(t, 2)
			app.config.SortByContext = true
			app.config.ForceReleaseDirectories = true
			started, release, failed, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			failureLogged := sync.OnceFunc(func() { close(failed) })
			fixture.trackStatus = func(id int64) int {
				if id == 1 {
					close(started)
					<-release
					return http.StatusOK
				}
				<-started
				return http.StatusForbidden
			}
			app.logWriter = collectionLogFunc(func(data []byte) (int, error) {
				if bytes.Contains(data, []byte("subscription required")) {
					failureLogged()
				}
				return len(data), nil
			})
			audio := audioFixture(t, "tone.flac")
			app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(audio)), Body: io.NopCloser(bytes.NewReader(audio))}, nil
			})}
			go func() { runCollection(app, bp, kind); close(done) }()
			t.Cleanup(func() {
				unblock()
				awaitWorkerEvent(t, done, "collection worker cleanup")
			})
			awaitWorkerEvent(t, failed, "second track's metadata failure")
			unblock()
			awaitWorkerEvent(t, done, "collection completion")
			assertCatalogFiles(t, filepath.Join(app.config.DownloadsDirectory, "Collection", "Release"), "11. Track 1.flac")
		})
	}
}

func TestChartCoverSurvivesReleaseCoverFailure(t *testing.T) {
	app, bp, fixture := newCollectionFixture(t, 1)
	app.config.SortByContext = true
	app.config.ForceReleaseDirectories = true
	app.config.KeepCover = true
	fixture.release.Image = beatport.Image{DynamicURI: "https://fixture.invalid/release-cover"}
	fixture.chartImage = beatport.Image{DynamicURI: "https://fixture.invalid/chart-cover"}
	audio := audioFixture(t, "tone.flac")
	app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
		status, data := http.StatusOK, audio
		if req.URL.Path == "/release-cover" {
			status, data = http.StatusNotFound, nil
		} else if req.URL.Path == "/chart-cover" {
			data = []byte("chart cover")
		}
		return &http.Response{StatusCode: status, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	runCollection(app, bp, "chart")
	dir := filepath.Join(app.config.DownloadsDirectory, "Collection")
	assertCatalogFiles(t, filepath.Join(dir, "Release"), "11. Track 1.flac")
	assertFileContents(t, filepath.Join(dir, "cover.jpg"), "chart cover")
	assertCatalogFiles(t, dir, "Release", "cover.jpg")
}

func TestConcurrentCollectionsMetadataFailureKeepsSharedDestinationUsable(t *testing.T) {
	for _, kind := range []string{"playlist", "chart"} {
		t.Run(kind, func(t *testing.T) {
			app, first, firstFixture := newCollectionFixture(t, 1)
			_, second, secondFixture := newCollectionFixture(t, 1)
			app.config.SortByContext = true
			app.config.ForceReleaseDirectories = true
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			firstFixture.trackStatus = func(int64) int {
				close(started)
				<-release
				return http.StatusOK
			}
			secondFixture.trackStatus = func(int64) int { return http.StatusForbidden }
			audio := audioFixture(t, "tone.flac")
			app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(audio)), Body: io.NopCloser(bytes.NewReader(audio))}, nil
			})}
			go func() { runCollection(app, first, kind); close(done) }()
			t.Cleanup(func() {
				unblock()
				awaitWorkerEvent(t, done, "concurrent collection cleanup")
			})
			awaitWorkerEvent(t, started, "first collection metadata request")
			runCollection(app, second, kind)
			unblock()
			awaitWorkerEvent(t, done, "first collection completion")
			assertCatalogFiles(t, filepath.Join(app.config.DownloadsDirectory, "Collection", "Release"), "11. Track 1.flac")
		})
	}
}

func BenchmarkExistingPlaylistSkipRequests(b *testing.B) {
	app, bp, fixture := newCollectionFixture(b, 1000)
	seedCollectionIdentities(b, app, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.resetTrackFileState()
		runCollection(app, bp, "playlist")
	}
	b.StopTimer()
	b.ReportMetric(float64(fixture.total())/float64(b.N), "requests/op")
}
