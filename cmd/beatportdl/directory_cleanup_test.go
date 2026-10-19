package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"unspok3n/beatportdl/internal/beatport"
)

func TestConcurrentTrackURLsCannotRemoveAnotherDownloadsDirectory(t *testing.T) {
	app, bp, fixture := newCollectionFixture(t, 2)
	app.bp = bp
	app.config.SortByContext = true
	app.globalSem = make(chan struct{}, 2)
	for i := range fixture.tracks {
		fixture.tracks[i].Genre = beatport.Genre{Name: "House"}
	}
	waiting, resume, failedURLDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(resume) })
	app.downloadTrack = func(_ *beatport.Beatport, id int64, _ string) (*beatport.TrackDownload, error) {
		if id == 1 {
			close(waiting)
			<-resume
			return &beatport.TrackDownload{Location: "https://fixture.invalid/audio", StreamQuality: ".flac"}, nil
		}
		return nil, errors.New("second track is unavailable")
	}
	audio := audioFixture(t, "tone.flac")
	app.mediaClient = &http.Client{Transport: searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(audio)), Body: io.NopCloser(bytes.NewReader(audio))}, nil
	})}
	app.globalWorker(func() { app.handleUrl("https://www.beatport.com/track/one/1") })
	t.Cleanup(func() { unblock(); app.wg.Wait() })
	awaitWorkerEvent(t, waiting, "first URL waiting for its download location")
	app.globalWorker(func() {
		app.handleUrl("https://www.beatport.com/track/two/2")
		close(failedURLDone)
	})
	awaitWorkerEvent(t, failedURLDone, "failed URL cleanup")
	dir := filepath.Join(app.config.DownloadsDirectory, "House")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("failed URL removed another download's directory: %v", err)
	}
	unblock()
	app.wg.Wait()
	assertCatalogFiles(t, dir, "11. Track 1.flac")
	got, err := os.ReadFile(filepath.Join(dir, "11. Track 1.flac"))
	if err != nil || !bytes.Equal(got, audio) {
		t.Fatalf("first URL's completed audio changed: err=%v", err)
	}
}

func TestDirectoryCleanupWaitsForAllURLWorkers(t *testing.T) {
	for _, outcome := range []string{"success", "panic", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			app := newTestApplication("skip")
			app.ctx, app.logWriter = ctx, io.Discard
			app.globalSem = make(chan struct{}, 2)
			app.config.DownloadsDirectory = t.TempDir()
			dir := filepath.Join(app.config.DownloadsDirectory, "Shared")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			started, resume, cleanupRequested := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(resume) })
			app.globalWorker(func() {
				close(started)
				if outcome == "cancel" {
					<-ctx.Done()
				} else {
					<-resume
				}
				if outcome == "panic" {
					panic("synthetic URL failure")
				}
			})
			t.Cleanup(func() { cancel(); unblock(); app.wg.Wait() })
			awaitWorkerEvent(t, started, "active URL worker")
			app.globalWorker(func() { app.cleanup(dir); close(cleanupRequested) })
			awaitWorkerEvent(t, cleanupRequested, "directory cleanup request")
			if _, err := os.Stat(dir); err != nil {
				t.Errorf("directory was removed while another URL was active: %v", err)
			}
			cancel()
			unblock()
			app.wg.Wait()
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unused directory was not cleaned after workers exited: %v", err)
			}
		})
	}
}

func TestDirectoryCleanupRemovesEmptyChildrenBeforeParents(t *testing.T) {
	app := newTestApplication("skip")
	app.config.DownloadsDirectory = t.TempDir()
	app.globalSem = make(chan struct{}, 1)
	parent := filepath.Join(app.config.DownloadsDirectory, "Context")
	child := filepath.Join(parent, "Release")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	app.globalWorker(func() {
		app.cleanup(parent)
		app.cleanup(child)
		app.cleanup(child) // Duplicate requests must be harmless.
	})
	app.wg.Wait()
	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty parent remained after child cleanup: %v", err)
	}
	if _, err := os.Stat(app.config.DownloadsDirectory); err != nil {
		t.Fatalf("configured download root was removed: %v", err)
	}
}

func TestDirectoryCleanupPreservesRootAndNonDirectoryEntries(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "standalone"
		if deferred {
			name = "URL worker"
		}
		t.Run(name, func(t *testing.T) {
			app := newTestApplication("skip")
			root := t.TempDir()
			app.config.DownloadsDirectory = root + string(os.PathSeparator) + "."
			app.globalSem = make(chan struct{}, 1)
			file := filepath.Join(root, "Audio.flac")
			if err := os.WriteFile(file, []byte("existing audio"), 0600); err != nil {
				t.Fatal(err)
			}
			nonempty := filepath.Join(root, "Existing")
			if err := os.Mkdir(nonempty, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(nonempty, "Track.flac"), []byte("completed track"), 0600); err != nil {
				t.Fatal(err)
			}
			cleanup := func() { app.cleanup(file); app.cleanup(nonempty); app.cleanup(root) }
			if deferred {
				app.globalWorker(cleanup)
				app.wg.Wait()
			} else {
				cleanup()
			}
			assertFileContents(t, file, "existing audio")
			assertFileContents(t, filepath.Join(nonempty, "Track.flac"), "completed track")
		})
	}
}

func TestDirectoryCleanupProtectsConfiguredRootAlias(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		app := newTestApplication("skip")
		root := t.TempDir()
		app.config.DownloadsDirectory = root + string(os.PathSeparator) + "."
		app.globalSem = make(chan struct{}, 1)
		if deferred {
			app.globalWorker(func() { app.cleanup(root) })
			app.wg.Wait()
		} else {
			app.cleanup(root)
		}
		if _, err := os.Stat(root); err != nil {
			t.Errorf("configured root alias was removed (deferred=%v): %v", deferred, err)
		}
	}
}

func TestDirectoryCleanupResetsAfterCancelledAdmissionAndFreshRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := newTestApplication("skip")
	app.ctx, app.logWriter = ctx, io.Discard
	app.config.DownloadsDirectory = t.TempDir()
	app.globalSem = make(chan struct{}, 1)
	app.globalWorker(func() { t.Error("cancelled URL was started") })
	app.wg.Wait()
	for run := 0; run < 2; run++ {
		app.ctx = context.Background()
		dir := filepath.Join(app.config.DownloadsDirectory, "Reused")
		app.globalWorker(func() {
			if _, err := app.createDirectory(dir); err != nil {
				t.Error(err)
			}
			app.cleanup(dir)
		})
		app.wg.Wait()
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("directory cleanup was not drained after run %d: %v", run, err)
		}
	}
}
