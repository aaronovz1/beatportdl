//go:build darwin || linux

package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestDownloadSegmentsClosesFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	var staged *syscall.Stat_t
	client := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = client })
	http.DefaultClient = &http.Client{Transport: segmentRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("expected one staging file, entries = %v, error = %v", entries, err)
		}
		info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		staged = info.Sys().(*syscall.Stat_t)
		return nil, errors.New("synthetic network failure")
	})}
	app := &application{}
	if got, err := app.downloadSegments(dir, []string{"https://example.test/segment"}, StreamKey{}, ""); got != "" || err == nil {
		t.Fatalf("downloadSegments() = %q, %v; want empty path and error", got, err)
	}
	if staged == nil {
		t.Fatal("request did not inspect the staging file")
	}
	// Unlinking succeeds even for open files on Unix, so check the descriptor too.
	descriptors, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	defer descriptors.Close()
	names, err := descriptors.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		var info syscall.Stat_t
		if syscall.Fstat(fd, &info) == nil && info.Dev == staged.Dev && info.Ino == staged.Ino {
			t.Errorf("staging file descriptor %d remains open after failure", fd)
		}
	}
	assertCatalogFiles(t, dir)
}
