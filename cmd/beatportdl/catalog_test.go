package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
	"unspok3n/beatportdl/internal/taglib"
)

func newCatalogTestApplication(t *testing.T) *application {
	t.Helper()
	app := newTestApplication("skip")
	app.ctx = context.Background()
	app.logWriter = io.Discard
	app.downloadSem = make(chan struct{}, 4)
	app.config.DownloadsDirectory = t.TempDir()
	app.config.SortByContext = true
	app.config.ForceReleaseDirectories = true
	app.config.KeepCover = true
	app.config.Quality = "lossless"
	app.config.ReleaseDirectoryTemplate = "{name}"
	app.config.TrackFileTemplate = "{name}"
	return app
}

func catalogTestSource(tracks []beatport.Track, release beatport.Release) catalogTrackSource {
	return catalogTrackSource{
		fetchPage: func(id int64, page int, params string) (*beatport.Paginated[beatport.Track], error) {
			return &beatport.Paginated[beatport.Track]{Results: tracks}, nil
		},
		fetchTrack: func(id int64) (*beatport.Track, error) {
			for _, track := range tracks {
				if track.ID == id {
					return &track, nil
				}
			}
			return nil, errors.New("unexpected nonmatching track")
		},
		fetchRelease: func(id int64) (*beatport.Release, error) { return &release, nil },
	}
}

func TestCatalogTracksOnlyDownloadsFilteredResults(t *testing.T) {
	app := newCatalogTestApplication(t)
	audio := audioFixture(t, "tone.flac")
	var coverRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover" {
			coverRequests.Add(1)
		}
		_, _ = w.Write(audio)
	}))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Mixed Genres", TrackCount: 3, Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
	// The release has three tracks, but only these two match the catalog filter.
	tracks := []beatport.Track{{ID: 101, Name: "One", Release: release}, {ID: 102, Name: "Two", Release: release}}
	source := catalogTestSource(tracks, release)
	var releaseRequests atomic.Int32
	source.fetchRelease = func(id int64) (*beatport.Release, error) {
		releaseRequests.Add(1)
		return &release, nil
	}
	source.fetchPage = func(id int64, page int, params string) (*beatport.Paginated[beatport.Track], error) {
		if id != 42 || params != "genre_name=bass+house" {
			return nil, errors.New("lost catalog scope or filter")
		}
		if page == 1 {
			next := "page2"
			return &beatport.Paginated[beatport.Track]{Results: tracks[:1], Next: &next}, nil
		}
		return &beatport.Paginated[beatport.Track]{Results: tracks[1:]}, nil
	}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{ID: 42, Params: "genre_name=bass+house"}, app.config.DownloadsDirectory, source); err != nil {
		t.Fatal(err)
	}
	assertCatalogFiles(t, filepath.Join(app.config.DownloadsDirectory, "Mixed Genres"), "One.flac", "Two.flac", "cover.jpg")
	if releaseRequests.Load() != 1 || coverRequests.Load() != 1 {
		t.Fatalf("release/cover requests = %d/%d, want 1/1", releaseRequests.Load(), coverRequests.Load())
	}
}

func TestCatalogTracksSkippedIdentityDoesNotDownloadCover(t *testing.T) {
	app := newCatalogTestApplication(t)
	app.config.FixTags = true
	app.config.CoverSize = "500x500"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("unexpected download"))
	}))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release", Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
	tracks := []beatport.Track{{ID: 101, Name: "Updated Name", Release: release}}
	dir := filepath.Join(app.config.DownloadsDirectory, "Release")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Original Name.flac"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	app.readTrackFileIdentity = func(path string) (trackFileIdentity, error) {
		return trackFileIdentity{TrackID: "101"}, nil
	}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{}, app.config.DownloadsDirectory, catalogTestSource(tracks, release)); err != nil {
		t.Fatal(err)
	}
	assertCatalogFiles(t, dir, "Original Name.flac")
	if requests.Load() != 0 {
		t.Fatalf("audio/cover requests for skipped track = %d, want 0", requests.Load())
	}
}

func TestCatalogTracksPreservesDistinctFilenameCollisions(t *testing.T) {
	app := newCatalogTestApplication(t)
	app.config.KeepCover = false
	firstAudio, secondAudio := audioFixture(t, "tone.flac"), audioFixture(t, "other-tone.flac")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/101" {
			_, _ = w.Write(firstAudio)
		} else {
			_, _ = w.Write(secondAudio)
		}
	}))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release"}
	tracks := []beatport.Track{{ID: 101, Name: "Same Name", Release: release}, {ID: 102, Name: "Same Name", Release: release}}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/" + strconv.FormatInt(id, 10), StreamQuality: ".flac"}, nil
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{}, app.config.DownloadsDirectory, catalogTestSource(tracks, release)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.config.DownloadsDirectory, "Release")
	assertCatalogFiles(t, dir, "Same Name.flac", "Same Name (1).flac")
	first, _ := os.ReadFile(filepath.Join(dir, "Same Name.flac"))
	second, _ := os.ReadFile(filepath.Join(dir, "Same Name (1).flac"))
	if string(first) == string(second) {
		t.Fatalf("distinct tracks have identical contents: %q", first)
	}
}

func TestCatalogTracksPaginationErrorWaitsBeforeCoverCleanup(t *testing.T) {
	app := newCatalogTestApplication(t)
	audio := audioFixture(t, "tone.flac")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(audio) }))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release", Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
	tracks := []beatport.Track{{ID: 101, Name: "One", Release: release}}
	source := catalogTestSource(tracks, release)
	started, unblock := make(chan struct{}), make(chan struct{})
	defer close(unblock)
	source.fetchTrack = func(id int64) (*beatport.Track, error) {
		close(started)
		<-unblock
		return &tracks[0], nil
	}
	pageError := errors.New("second page failed")
	source.fetchPage = func(id int64, page int, params string) (*beatport.Paginated[beatport.Track], error) {
		if page == 2 {
			<-started
			return nil, pageError
		}
		next := "page2"
		return &beatport.Paginated[beatport.Track]{Results: tracks, Next: &next}, nil
	}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
	}
	done := make(chan error, 1)
	go func() { done <- app.handleCatalogTracks(nil, &beatport.Link{}, app.config.DownloadsDirectory, source) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before worker finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, pageError) {
			t.Fatalf("error = %v, want page failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish")
	}
	assertCatalogFiles(t, filepath.Join(app.config.DownloadsDirectory, "Release"), "One.flac", "cover.jpg")
}

func TestCatalogTracksTagFailureCleansTemporaryCover(t *testing.T) {
	app := newCatalogTestApplication(t)
	audio := audioFixture(t, "tone.flac")
	app.config.FixTags = true
	app.config.CoverSize = "500x500"
	var coverRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover" {
			coverRequests.Add(1)
			// Inject a tag-read failure after transfer validation, while the cover is fetched.
			if err := os.WriteFile(filepath.Join(app.config.DownloadsDirectory, "Release", "One.flac"), []byte("invalid audio"), 0600); err != nil {
				t.Error(err)
			}
		}
		_, _ = w.Write(audio)
	}))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release", Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
	tracks := []beatport.Track{{ID: 101, Name: "One", Release: release}}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{}, app.config.DownloadsDirectory, catalogTestSource(tracks, release)); err != nil {
		t.Fatal(err)
	}
	assertCatalogFiles(t, filepath.Join(app.config.DownloadsDirectory, "Release"), "One.flac")
	if coverRequests.Load() != 1 {
		t.Fatalf("cover requests = %d, want 1 before tag failure", coverRequests.Load())
	}
}

func TestCatalogTracksFailedAudioDoesNotRequestCover(t *testing.T) {
	app := newCatalogTestApplication(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release", Image: beatport.Image{DynamicURI: "invalid-if-cover-requested"}}
	tracks := []beatport.Track{{ID: 101, Name: "One", Release: release}}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL, StreamQuality: ".flac"}, nil
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{}, app.config.DownloadsDirectory, catalogTestSource(tracks, release)); err != nil {
		t.Fatal(err)
	}
	assertCatalogFiles(t, app.config.DownloadsDirectory)
}

func TestArtistAndLabelTracksDirectoryLayout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		linkType   beatport.LinkType
		force      bool
		sort       bool
		wantNested bool
	}{
		{"artist default", beatport.ArtistLink, false, true, false},
		{"artist forced releases", beatport.ArtistLink, true, true, true},
		{"artist sorting disabled", beatport.ArtistLink, true, false, false},
		{"label default", beatport.LabelLink, false, true, false},
		{"label forced releases", beatport.LabelLink, true, true, true},
		{"label sorting disabled", beatport.LabelLink, false, false, false},
		{"label sorting disabled with force", beatport.LabelLink, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCatalogTestApplication(t)
			audio := audioFixture(t, "tone.flac")
			app.config.SortByContext = tc.sort
			app.config.ForceReleaseDirectories = tc.force
			app.config.ArtistDirectoryTemplate = "{name}"
			app.config.LabelDirectoryTemplate = "{name}"
			var entity DownloadsDirectoryEntity = &beatport.Artist{Name: "Artist"}
			if tc.linkType == beatport.LabelLink {
				entity = &beatport.Label{Name: "Label"}
			}
			dir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, entity)
			if err != nil {
				t.Fatal(err)
			}
			wantDir := app.config.DownloadsDirectory
			if tc.sort {
				group, name := "Artists", "Artist"
				if tc.linkType == beatport.LabelLink {
					group, name = "Labels", "Label"
				}
				wantDir = filepath.Join(wantDir, group, name)
			}
			if dir != wantDir {
				t.Errorf("context directory = %q, want %q", dir, wantDir)
			}
			var covers atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cover" {
					covers.Add(1)
				}
				_, _ = w.Write(audio)
			}))
			defer server.Close()
			first := beatport.Release{ID: 9, Name: "First Release", Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
			second := beatport.Release{ID: 10, Name: "Second Release", Image: first.Image}
			tracks := []beatport.Track{{ID: 101, Name: "One", Release: first}, {ID: 102, Name: "Two", Release: second}}
			source := catalogTestSource(tracks, first)
			source.fetchRelease = func(id int64) (*beatport.Release, error) {
				if id == first.ID {
					return &first, nil
				}
				return &second, nil
			}
			app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
				return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
			}
			if err := app.handleCatalogTracks(nil, &beatport.Link{Type: tc.linkType}, dir, source); err != nil {
				t.Fatal(err)
			}
			if tc.wantNested {
				assertCatalogFiles(t, dir, "First Release", "Second Release")
				assertCatalogFiles(t, filepath.Join(dir, "First Release"), "One.flac", "cover.jpg")
				assertCatalogFiles(t, filepath.Join(dir, "Second Release"), "Two.flac", "cover.jpg")
				if covers.Load() != 2 {
					t.Errorf("cover downloads = %d, want 2", covers.Load())
				}
			} else {
				assertCatalogFiles(t, dir, "One.flac", "Two.flac")
				if covers.Load() != 0 {
					t.Errorf("cover downloads = %d, want 0 for flat layout without tag fixes", covers.Load())
				}
			}
		})
	}
}

func TestFlatCatalogTracksPreservesEmbeddedCoverAndIdentitySkip(t *testing.T) {
	for _, linkType := range []beatport.LinkType{beatport.ArtistLink, beatport.LabelLink} {
		t.Run(string(linkType), func(t *testing.T) { testFlatCatalogTracksPreservesEmbeddedCoverAndIdentitySkip(t, linkType) })
	}
}

func testFlatCatalogTracksPreservesEmbeddedCoverAndIdentitySkip(t *testing.T, linkType beatport.LinkType) {
	app := newCatalogTestApplication(t)
	app.config.ForceReleaseDirectories = false
	app.config.ArtistDirectoryTemplate = "{name}"
	app.config.LabelDirectoryTemplate = "{name}"
	app.config.FixTags = true
	app.config.CoverSize = "500x500"
	app.config.TagMappings = config.DefaultTagMappings
	// Metadata-only FLAC fixture: STREAMINFO followed by an empty VORBIS_COMMENT block.
	audio := make([]byte, 58)
	copy(audio, "fLaC")
	audio[7] = 34
	binary.BigEndian.PutUint16(audio[8:10], 16)
	binary.BigEndian.PutUint16(audio[10:12], 16)
	binary.BigEndian.PutUint64(audio[18:26], uint64(44100)<<44|uint64(15)<<36)
	audio[42], audio[45] = 0x84, 12
	binary.LittleEndian.PutUint32(audio[46:50], 4)
	copy(audio[50:54], "test")
	cover := []byte("cover metadata fixture")
	var audioRequests, coverRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover" {
			coverRequests.Add(1)
			_, _ = w.Write(cover)
		} else {
			audioRequests.Add(1)
			_, _ = w.Write(audio)
		}
	}))
	defer server.Close()
	release := beatport.Release{ID: 9, Name: "Release", Image: beatport.Image{DynamicURI: server.URL + "/cover"}}
	track := beatport.Track{ID: 101, ISRC: "GBABC2600001", Name: "One", Release: release}
	app.downloadTrack = func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: server.URL + "/audio", StreamQuality: ".flac"}, nil
	}
	link := &beatport.Link{Type: linkType}
	var entity DownloadsDirectoryEntity = &beatport.Artist{Name: "Example"}
	if linkType == beatport.LabelLink {
		entity = &beatport.Label{Name: "Example"}
	}
	dir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, entity)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.handleCatalogTracks(nil, link, dir, catalogTestSource([]beatport.Track{track}, release)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "One.flac")
	file, err := taglib.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	picture, err := file.GetPicture()
	if err != nil || !bytes.Equal(picture.Data, cover) {
		file.Close()
		t.Fatalf("embedded cover mismatch: %v", err)
	}
	file.Close()
	identity, err := app.readExistingTrackFileIdentity(path)
	if err != nil || identity.ISRC != track.ISRC {
		t.Fatalf("identity = %+v, error = %v", identity, err)
	}
	assertCatalogFiles(t, dir, "One.flac")
	// A fresh run should recognize the tagged file despite a metadata filename change.
	app.activeFiles = make(map[string]int64)
	app.trackIdentityFiles = nil
	track.Name = "Updated Name"
	if err := app.handleCatalogTracks(nil, link, dir, catalogTestSource([]beatport.Track{track}, release)); err != nil {
		t.Fatal(err)
	}
	assertCatalogFiles(t, dir, "One.flac")
	if audioRequests.Load() != 1 || coverRequests.Load() != 1 {
		t.Fatalf("audio/cover requests = %d/%d, want 1/1 across both runs", audioRequests.Load(), coverRequests.Load())
	}
}

func assertCatalogFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for _, name := range names {
		want[name] = true
	}
	for _, entry := range entries {
		if !want[entry.Name()] {
			t.Errorf("unexpected file %q (possible temporary cover leak)", entry.Name())
		}
		delete(want, entry.Name())
	}
	for name := range want {
		t.Errorf("missing file %q", name)
	}
}
