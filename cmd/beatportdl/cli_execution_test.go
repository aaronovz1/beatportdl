package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestCLIBatchReportsWorkerFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		jobs []string
		want uint64
	}{
		{"success", []string{"success"}, 0},
		{"mixed", []string{"success", "failure", "panic"}, 2},
		{"all failed", []string{"failure", "failure"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &application{
				logWriter: io.Discard, globalSem: make(chan struct{}, 2), downloadSem: make(chan struct{}, 2),
			}
			var handled atomic.Int32
			err := app.runJobs(nil, tc.jobs, true, func(job string) {
				defer handled.Add(1)
				switch job {
				case "failure":
					app.LogError("fixture worker", errors.New("fixture error"))
				case "panic":
					panic("fixture panic")
				}
			})
			if handled.Load() != int32(len(tc.jobs)) || app.failures.Load() != tc.want || (err != nil) != (tc.want != 0) {
				t.Fatalf("handled=%d failures=%d err=%v", handled.Load(), app.failures.Load(), err)
			}
			if app.logWriter != io.Discard || app.pbp != nil {
				t.Fatal("batch did not restore its writer and progress state")
			}
		})
	}
}

func TestCLITextListReportsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch.txt")
	list := "before\n" + strings.Repeat("x", bufio.MaxScanTokenSize+1) + "\nnot read\n"
	if err := os.WriteFile(path, []byte(list), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := &application{logWriter: &output, globalSem: make(chan struct{}, 1)}
	var handled []string
	err := app.runJobs(nil, []string{path, "after"}, true, func(job string) {
		handled = append(handled, job)
	})
	if err == nil || !strings.Contains(output.String(), "line 2") || !reflect.DeepEqual(handled, []string{"before", "after"}) {
		t.Fatalf("err=%v, output=%q, completed=%q", err, output.String(), handled)
	}
}

func TestCLIPipedPromptsReuseReaderThroughEOF(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	if _, err := writer.WriteString("https://example.test/one\r\nhttps://example.test/two"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	app := &application{logWriter: io.Discard, globalSem: make(chan struct{}, 1)}
	var handled []string
	err = app.runJobs(newCLIInput(context.Background(), input), nil, false, func(url string) {
		handled = append(handled, url)
	})
	if err != nil || !reflect.DeepEqual(handled, []string{"https://example.test/one", "https://example.test/two"}) {
		t.Fatalf("handled=%q, err=%v", handled, err)
	}
}

func TestCLICancelIdlePrompt(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	app := &application{ctx: ctx, logWriter: io.Discard}
	if err := app.runJobs(newCLIInput(ctx, input), nil, false, func(string) {
		t.Error("unexpected job")
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prompt cancellation = %v", err)
	}
}

func TestCLICancelActiveBatchJoinsWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app := &application{ctx: ctx, logWriter: io.Discard, globalSem: make(chan struct{}, 1)}
	started, stopping, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseWorker := sync.OnceFunc(func() { close(release) })
	defer releaseWorker()
	done := make(chan error, 1)
	go func() {
		done <- app.runJobs(nil, []string{"active", "queued"}, true, func(job string) {
			if job != "active" {
				t.Error("queued worker ran after cancellation")
				return
			}
			close(started)
			<-ctx.Done()
			close(stopping)
			<-release
			app.LogInfo("fixture cleanup finished")
		})
	}()
	wait := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not reach cancellation checkpoint")
		}
	}
	wait(started)
	cancel()
	wait(stopping)
	select {
	case err := <-done:
		t.Fatalf("returned before worker cleanup: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	releaseWorker()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || app.pbp != nil || app.logWriter != io.Discard {
			t.Fatalf("run did not cancel and restore writer: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run hung after worker cleanup")
	}
}

func TestCLISignalDuringStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending Unix signals to test processes is unavailable")
	}
	if os.Getenv("BEATPORTDL_TEST_STARTUP_SIGNAL") == "1" {
		os.Args = []string{"beatportdl", "--password=fixture", "--proxy-stdin"}
		main()
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLISignalDuringStartup$")
	cmd.Env = append(os.Environ(), "BEATPORTDL_TEST_STARTUP_SIGNAL=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(stderr)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, "--password exposes") {
		t.Fatalf("startup not ready: %q, %v", line, err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 130 || !strings.Contains(string(rest), "Shutdown signal received") {
		t.Fatalf("interruption status = %v; stderr: %q", err, rest)
	}
}

func TestCLIErrorPresentationEscapesControls(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "fixture.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var console bytes.Buffer
	app := &application{logWriter: &console, logFile: file}
	app.LogError("fixture\x1b", errors.New("Caf\u00e9\r\nname\u202e"))
	want := `fixture\x1b: Caf` + "\u00e9" + `\r\nname\u202e` + "\n"
	persisted, err := os.ReadFile(file.Name())
	if err != nil || console.String() != want || string(persisted) != want {
		t.Fatalf("console=%q, log=%q, err=%v", console.String(), persisted, err)
	}
}
