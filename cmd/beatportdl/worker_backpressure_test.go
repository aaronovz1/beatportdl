package main

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type backpressureWorker struct {
	cancel context.CancelFunc
	sem    chan struct{}
	submit func(func())
	wait   func()
}

func newBackpressureWorker(global bool, capacity int) backpressureWorker {
	ctx, cancel := context.WithCancel(context.Background())
	sem := make(chan struct{}, capacity)
	app := &application{ctx: ctx, globalSem: sem, downloadSem: sem, logWriter: io.Discard}
	if global {
		return backpressureWorker{cancel, sem, app.globalWorker, app.wg.Wait}
	}
	var wg sync.WaitGroup
	return backpressureWorker{cancel, sem, func(fn func()) { app.downloadWorker(&wg, fn) }, wg.Wait}
}

func awaitWorkerEvent(t *testing.T, event <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func releaseBlockedWorker(t *testing.T, release chan<- struct{}) {
	t.Helper()
	select {
	case release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out releasing an admitted worker")
	}
}

func TestWorkerBackpressure10K(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "download"
		if global {
			name = "global"
		}
		for _, capacity := range []int{1, 4} {
			t.Run(fmt.Sprintf("%s/capacity_%d", name, capacity), func(t *testing.T) {
				const jobs = 10000
				worker := newBackpressureWorker(global, capacity)
				started := make(chan struct{}, jobs)
				submitted := make(chan struct{}, jobs)
				release := make(chan struct{})
				producerDone := make(chan struct{})
				var callbacks atomic.Int64
				var progress atomic.Int64
				baseline := runtime.NumGoroutine()
				go func() {
					defer close(producerDone)
					for i := 0; i < jobs; i++ {
						worker.submit(func() {
							callbacks.Add(1)
							started <- struct{}{}
							<-release
						})
						progress.Add(1)
						submitted <- struct{}{}
					}
				}()
				t.Cleanup(func() {
					worker.cancel()
					close(release)
					awaitWorkerEvent(t, producerDone, "producer cleanup")
					joined := make(chan struct{})
					go func() { worker.wait(); close(joined) }()
					awaitWorkerEvent(t, joined, "worker cleanup")
				})

				for i := 0; i < capacity; i++ {
					awaitWorkerEvent(t, started, "worker start")
					awaitWorkerEvent(t, submitted, "accepted submission")
				}
				// An extra submission is a failure event; the timer only bounds
				// observation while all admitted callbacks are held at release.
				select {
				case <-submitted:
					awaitWorkerEvent(t, producerDone, "unbounded producer completion")
				case <-time.After(100 * time.Millisecond):
				}
				if got := progress.Load(); got != int64(capacity) {
					t.Errorf("blocked producer submitted %d jobs, want %d", got, capacity)
				}
				if growth := runtime.NumGoroutine() - baseline; growth > capacity+8 {
					t.Errorf("blocked %d-job queue added %d goroutines, want at most %d", jobs, growth, capacity+8)
				}
				if got := len(worker.sem); got != capacity {
					t.Errorf("occupied slots = %d, want %d", got, capacity)
				}
				if got := callbacks.Load(); got != int64(capacity) {
					t.Errorf("running callbacks = %d, want %d", got, capacity)
				}
				if t.Failed() {
					return
				}

				// Completing one callback must admit exactly one more job.
				releaseBlockedWorker(t, release)
				awaitWorkerEvent(t, started, "replacement worker start")
				awaitWorkerEvent(t, submitted, "replacement submission")
				worker.cancel()
				awaitWorkerEvent(t, producerDone, "cancelled producer completion")
				joined := make(chan struct{})
				go func() { worker.wait(); close(joined) }()
				select {
				case <-joined:
					t.Error("worker join returned while admitted callbacks were blocked")
				case <-time.After(100 * time.Millisecond):
				}
				for i := 0; i < capacity; i++ {
					releaseBlockedWorker(t, release)
				}
				awaitWorkerEvent(t, joined, "admitted worker completion")
				if got := callbacks.Load(); got != int64(capacity+1) {
					t.Errorf("callbacks = %d, want %d; cancelled submissions must not run", got, capacity+1)
				}
				if got := len(worker.sem); got != 0 {
					t.Errorf("slots after join = %d, want 0", got)
				}
			})
		}
	}
}

func TestWorkerPanicReleasesSlot(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "download"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			worker := newBackpressureWorker(global, 1)
			defer worker.cancel()
			replacementRan := make(chan struct{})
			producerDone := make(chan struct{})
			go func() {
				defer close(producerDone)
				worker.submit(func() { panic("worker test panic") })
				worker.submit(func() { close(replacementRan) })
			}()
			awaitWorkerEvent(t, replacementRan, "replacement after panic")
			awaitWorkerEvent(t, producerDone, "panic test producer")
			joined := make(chan struct{})
			go func() { worker.wait(); close(joined) }()
			awaitWorkerEvent(t, joined, "panic test worker join")
			if got := len(worker.sem); got != 0 {
				t.Errorf("slots after panic and join = %d, want 0", got)
			}
		})
	}
}
