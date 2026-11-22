package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vbauerster/mpb/v8"
	"unspok3n/beatportdl/internal/beatport"
	"unspok3n/beatportdl/internal/taglib"
)

func TestPaginationRejectsRepeatedContinuation(t *testing.T) {
	var wg sync.WaitGroup
	var processed int
	next := "?page=2"
	err := forPaginatedWorkers(&wg, 42, "", func(_ int64, page int, _ string) (*beatport.Paginated[int], error) {
		if page > 2 {
			return nil, fmt.Errorf("requested unexpected page %d", page)
		}
		return &beatport.Paginated[int]{Results: []int{1}, Next: &next}, nil
	}, func(_ int, _ int) error { processed++; return nil })
	if err == nil || !strings.Contains(err.Error(), "pagination") || processed != 1 {
		t.Fatalf("error = %v, processed = %d", err, processed)
	}
}

func TestFlatCatalogSkipAvoidsMetadata(t *testing.T) {
	app := newCatalogTestApplication(t)
	app.config.ForceReleaseDirectories = false
	dir := app.config.DownloadsDirectory
	writeIdentityAudio(t, filepath.Join(dir, "Existing.flac"), "", "GBABC2600001")
	var lookups atomic.Int32
	app.downloadTrack = func(*beatport.Beatport, int64, string) (*beatport.TrackDownload, error) {
		lookups.Add(1)
		return &beatport.TrackDownload{Location: "https://fixture.invalid/never-fetched", StreamQuality: ".flac"}, nil
	}
	source := catalogTestSource([]beatport.Track{{ID: 101, ISRC: "GBABC2600001"}}, beatport.Release{})
	var details atomic.Int32
	source.fetchTrack = func(int64) (*beatport.Track, error) { details.Add(1); return nil, fmt.Errorf("unneeded track lookup") }
	source.fetchRelease = func(int64) (*beatport.Release, error) {
		details.Add(1)
		return nil, fmt.Errorf("unneeded release lookup")
	}
	if err := app.handleCatalogTracks(nil, &beatport.Link{ID: 42}, dir, source); err != nil {
		t.Fatal(err)
	}
	if lookups.Load() != 1 || details.Load() != 0 {
		t.Fatalf("access/detail calls: %d/%d, want 1/0", lookups.Load(), details.Load())
	}
	assertCatalogFiles(t, dir, "Existing.flac")
}

func TestCollectionSharesCoverForActualAACFormat(t *testing.T) {
	app, bp, fixture := newCollectionFixture(t, 2)
	app.config.FixTags = true
	fixture.quality = ".256k.aac.mp4"
	fixture.release.Image.DynamicURI = "https://fixture.invalid/cover"
	audio := audioFixture(t, "tone.m4a")
	var covers atomic.Int32
	app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(r *http.Request) (*http.Response, error) {
		body := audio
		if r.URL.Path == "/cover" {
			covers.Add(1)
			body = []byte("cover fixture")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	})}
	runCollection(app, bp, "playlist")
	if covers.Load() != 1 {
		t.Fatalf("cover requests = %d, want one shared request", covers.Load())
	}
	dir := app.config.DownloadsDirectory
	assertCatalogFiles(t, dir, "11. Track 1.m4a", "12. Track 2.m4a")
	for _, name := range []string{"11. Track 1.m4a", "12. Track 2.m4a"} {
		f, err := taglib.Read(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		picture, err := f.GetPicture()
		f.Close()
		if err != nil || string(picture.Data) != "cover fixture" {
			t.Fatalf("%s: embedded cover unavailable: %v", name, err)
		}
	}
}

func TestNamedCoversDoNotReplaceEachOtherInSharedFolder(t *testing.T) {
	app := newCatalogTestApplication(t)
	dir := app.config.DownloadsDirectory
	for i, content := range []string{"first release", "second release"} {
		original := filepath.Join(dir, fmt.Sprintf("cover-fixture-%d", i))
		if err := os.WriteFile(original, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		cover := &trackCover{app: app, download: func() (string, error) { return original, nil }}
		if err := cover.handleNamed(fmt.Sprintf("cover-%d.jpg", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	assertFileContents(t, filepath.Join(dir, "cover-1.jpg"), "first release")
	assertFileContents(t, filepath.Join(dir, "cover-2.jpg"), "second release")
	assertCatalogFiles(t, dir, "cover-1.jpg", "cover-2.jpg")
}

func TestCoverDownloadRejectsOversizeWithoutPublishing(t *testing.T) {
	for _, declared := range []bool{true, false} {
		t.Run(fmt.Sprint(declared), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				const size = 16*1024*1024 + 1
				if declared {
					w.Header().Set("Content-Length", fmt.Sprint(size))
				} else {
					w.(http.Flusher).Flush()
				}
				chunk := make([]byte, 32*1024)
				for left := size; left > 0; {
					n, err := w.Write(chunk[:min(left, len(chunk))])
					left -= n
					if err != nil {
						return
					}
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			app := directDownloadTestApplication(server.URL)
			_, err := app.downloadCover(beatport.Image{DynamicURI: server.URL}, dir)
			if err == nil || !strings.Contains(err.Error(), "limit") {
				t.Fatalf("oversize cover returned %v", err)
			}
			assertCatalogFiles(t, dir)
		})
	}
}

func TestHLSSegmentDecryptsInPlace(t *testing.T) {
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	padded := append([]byte("1234567890abcdef"), bytes.Repeat([]byte{16}, 16)...)
	ciphertext := encryptPaddedSegment(t, padded, key)
	got, err := decryptSegment(ciphertext, key)
	if err != nil || string(got) != "1234567890abcdef" || &got[0] != &ciphertext[0] {
		t.Fatalf("expected in-place plaintext, got %q, %v", got, err)
	}
}

func TestFFmpegDiagnosticsAreBounded(t *testing.T) {
	var diagnostic ffmpegDiagnostic
	input := bytes.Repeat([]byte("diagnostic\n"), 5000)
	if n, err := diagnostic.Write(input); n != len(input) || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	if got := diagnostic.String(); len(got) > maxFFmpegDiagnostic+32 || !strings.HasSuffix(got, "[output truncated]") {
		t.Fatalf("unexpected bounded diagnostics: length %d", len(got))
	}
}

func TestFailedHLSProgressDoesNotWaitForever(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	progress := mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(io.Discard))
	app := &application{pbp: progress}
	if _, err := app.downloadSegments(t.TempDir(), []string{server.URL}, StreamKey{}, "Track"); err == nil {
		t.Fatal("bad segment accepted")
	}
	done := make(chan struct{})
	go func() { progress.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		progress.Shutdown()
		t.Fatal("failed download left a live progress bar")
	}
}

func TestFFmpegRunnerHonorsDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell ffmpeg test double")
	}
	// The timeout must cancel the process even when the original caller would wait.
	dir := t.TempDir()
	installFFmpegStub(t, "while :; do :; done")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := validateM4AContext(ctx, filepath.Join(dir, "audio.m4a")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline returned %v", err)
	}
}
