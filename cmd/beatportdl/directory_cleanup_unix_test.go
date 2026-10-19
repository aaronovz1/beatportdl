//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryCleanupPreservesSymlinksAndRootReferent(t *testing.T) {
	app := newTestApplication("skip")
	app.config.DownloadsDirectory = t.TempDir()
	app.globalSem = make(chan struct{}, 1)
	target := t.TempDir()
	link := filepath.Join(app.config.DownloadsDirectory, "Linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	app.globalWorker(func() { app.cleanup(link) })
	app.wg.Wait()
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("directory cleanup removed symlink: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	}
	app.config.DownloadsDirectory = link
	app.globalWorker(func() { app.cleanup(target) })
	app.wg.Wait()
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("configured root's referent was removed: %v", err)
	}
}
