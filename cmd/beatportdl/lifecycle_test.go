package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unspok3n/beatportdl/internal/beatport"
)

func TestQueuedWorkersExitOnCancellationWithoutTakingSlot(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "download"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sem := make(chan struct{}, 1)
			sem <- struct{}{}
			app := &application{ctx: ctx, logWriter: io.Discard, downloadSem: sem, globalSem: sem}
			var wg sync.WaitGroup
			var ran atomic.Bool
			if global {
				app.globalWorker(func() { ran.Store(true) })
			} else {
				app.downloadWorker(&wg, func() { ran.Store(true) })
			}
			// Establish that the worker has passed startup and is waiting for a slot.
			deadline := time.Now().Add(3 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				stack := make([]byte, 1<<16)
				n := runtime.Stack(stack, true)
				if bytes.Contains(stack[:n], []byte("(*application).semAcquire")) {
					blocked = true
					break
				}
				runtime.Gosched()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); app.wg.Wait(); close(done) }()
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				<-sem
				<-done
				t.Fatal("cancelled worker remained blocked waiting for a slot")
			}
			if !blocked {
				t.Fatal("worker never reached slot acquisition")
			}
			if ran.Load() || len(sem) != 1 {
				t.Fatalf("cancelled worker ran=%v, occupied slots=%d; want false and 1", ran.Load(), len(sem))
			}
			<-sem
		})
	}
}

func TestCollectionPaginationJoinsWorkersBeforeReturning(t *testing.T) {
	t.Run("playlist", func(t *testing.T) { testCollectionWorkerJoin[beatport.PlaylistItem](t) })
	t.Run("chart", func(t *testing.T) { testCollectionWorkerJoin[beatport.Track](t) })
}

func testCollectionWorkerJoin[T any](t *testing.T) {
	t.Helper()
	for _, outcome := range []string{"success", "later page error", "processing error"} {
		t.Run(outcome, func(t *testing.T) {
			app := &application{ctx: context.Background(), downloadSem: make(chan struct{}, 1), logWriter: io.Discard}
			var wg sync.WaitGroup
			started, release, paginated := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var workerFinished atomic.Bool
			failure := errors.New(outcome)
			var item T
			fetch := func(id int64, page int, params string) (*beatport.Paginated[T], error) {
				if page == 2 {
					close(paginated)
					return nil, failure
				}
				result := &beatport.Paginated[T]{Results: []T{item}}
				if outcome == "later page error" {
					next := "page2"
					result.Next = &next
				} else if outcome == "processing error" {
					result.Results = append(result.Results, item)
				}
				return result, nil
			}
			process := func(item T, i int) error {
				if i == 1 {
					close(paginated)
					return failure
				}
				app.downloadWorker(&wg, func() { close(started); <-release; workerFinished.Store(true) })
				if outcome == "success" {
					close(paginated)
				}
				return nil
			}
			done := make(chan error, 1)
			go func() { done <- forPaginatedWorkers[T](&wg, 42, "", fetch, process) }()
			<-started
			<-paginated
			returnedEarly := false
			var got error
			select {
			case got = <-done:
				returnedEarly = true
			case <-time.After(30 * time.Millisecond):
			}
			close(release)
			if !returnedEarly {
				got = <-done
			}
			wg.Wait()
			if returnedEarly || !workerFinished.Load() {
				t.Error("pagination returned before its worker completed")
			}
			if outcome == "success" && got != nil || outcome != "success" && !errors.Is(got, failure) {
				t.Fatalf("pagination error = %v, outcome = %s", got, outcome)
			}
		})
	}
}

func TestMediaCancellationPreservesExistingFileAndCleansStaging(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		name := "waiting headers"
		if bodyStarted {
			name = "reading body"
		}
		t.Run(name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			releaseRequest := sync.OnceFunc(func() { close(release) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if bodyStarted {
					w.Header().Set("Content-Length", "10000")
					_, _ = w.Write([]byte("partial"))
					w.(http.Flusher).Flush()
				}
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer releaseRequest()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dir := t.TempDir()
			path := filepath.Join(dir, "Existing.flac")
			if err := os.WriteFile(path, []byte("original audio"), 0600); err != nil {
				t.Fatal(err)
			}
			app := &application{ctx: ctx}
			done := make(chan error, 1)
			go func() { done <- app.downloadFile(server.URL, path, "") }()
			<-started
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("download error = %v, want context cancellation", err)
				}
			case <-time.After(2 * time.Second):
				releaseRequest()
				<-done
				t.Fatal("media transfer ignored cancellation")
			}
			assertFileContents(t, path, "original audio")
			assertCatalogFiles(t, dir, "Existing.flac")
		})
	}
}

func TestMediaTimeoutsAreBoundedAndPreserveDestination(t *testing.T) {
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			release := make(chan struct{})
			releaseRequest := sync.OnceFunc(func() { close(release) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "body" {
					_, _ = w.Write([]byte("partial"))
					w.(http.Flusher).Flush()
				}
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			defer releaseRequest()
			client := newMediaClient()
			if phase == "headers" {
				client.Transport.(*http.Transport).ResponseHeaderTimeout = 40 * time.Millisecond
			} else {
				client.Timeout = 40 * time.Millisecond
			}
			defer client.CloseIdleConnections()
			app := &application{mediaClient: client}
			dir := t.TempDir()
			path := filepath.Join(dir, "Existing.flac")
			if err := os.WriteFile(path, []byte("original audio"), 0600); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- app.downloadFile(server.URL, path, "") }()
			var err error
			select {
			case err = <-done:
			case <-time.After(2 * time.Second):
				releaseRequest()
				<-done
				t.Fatal("media transfer ignored its timeout")
			}
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Fatalf("download error = %v, want timeout", err)
			}
			assertFileContents(t, path, "original audio")
			assertCatalogFiles(t, dir, "Existing.flac")
		})
	}
}

func TestMediaClientAllowsLargeTransfersWithBoundedHeaders(t *testing.T) {
	client := newMediaClient()
	defer client.CloseIdleConnections()
	if client.Timeout != 30*time.Minute {
		t.Fatalf("overall timeout = %s, want 30m", client.Timeout)
	}
	if got := client.Transport.(*http.Transport).ResponseHeaderTimeout; got != 40*time.Second {
		t.Fatalf("header timeout = %s, want 40s", got)
	}
}

func TestCancellationStopsCoversAndEveryHLSRequest(t *testing.T) {
	for _, phase := range []string{"cover", "playlist", "key", "segment"} {
		t.Run(phase, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			key := StreamKey{Value: make([]byte, 16), IV: make([]byte, 16)}
			encrypted := encryptPaddedSegment(t, append([]byte("1234567890abcdef"), bytes.Repeat([]byte{16}, 16)...), key)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hls/"+phase {
					if phase == "key" {
						w.Header().Set("Content-Length", "16")
						_, _ = w.Write([]byte{0})
						w.(http.Flusher).Flush()
					}
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				if r.URL.Path == "/hls/playlist" {
					fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x00000000000000000000000000000000\n#EXTINF:10,\nsegment\n#EXT-X-ENDLIST\n")
				} else {
					_, _ = w.Write(encrypted)
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			app := newTestApplication("overwrite")
			app.ctx = ctx
			dir := t.TempDir()
			done := make(chan error, 1)
			go func() {
				var err error
				switch phase {
				case "cover":
					_, err = app.downloadCover(beatport.Image{DynamicURI: server.URL + "/hls/cover"}, dir)
				case "playlist", "key":
					_, _, err = app.getStreamSegments(server.URL + "/hls/playlist")
				case "segment":
					_, err = app.downloadSegments(dir, []string{server.URL + "/hls/one", server.URL + "/hls/segment"}, key, "")
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not reach cancellation point")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("request error = %v, want cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request ignored cancellation")
			}
			assertCatalogFiles(t, dir)
		})
	}
}

func TestCancellationStopsRemuxAndRetainsOriginal(t *testing.T) {
	signals := t.TempDir()
	started, release := filepath.Join(signals, "started"), filepath.Join(signals, "release")
	t.Setenv("BPD_TEST_STARTED", started)
	t.Setenv("BPD_TEST_RELEASE", release)
	installFFmpegStub(t, `printf ready > "$BPD_TEST_STARTED"
while [ ! -f "$BPD_TEST_RELEASE" ]; do :; done
exit 1
`)
	defer os.WriteFile(release, nil, 0600)
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.ts"), filepath.Join(dir, "Existing.m4a")
	if err := os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("original audio"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- remuxToM4AContext(ctx, input, output) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ffmpeg stub did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ctx.Err()) {
			t.Fatalf("remux error = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		_ = os.WriteFile(release, nil, 0600)
		<-done
		t.Fatal("ffmpeg ignored cancellation")
	}
	assertFileContents(t, output, "original audio")
	assertCatalogFiles(t, dir, "input.ts", "Existing.m4a")
}
