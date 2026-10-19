package main

import (
	"os"
	"path/filepath"
	"sort"
)

func (app *application) beginURLWorker() {
	app.directoryCleanupMutex.Lock()
	defer app.directoryCleanupMutex.Unlock()
	app.activeURLWorkers++
}

func (app *application) endURLWorker() {
	app.directoryCleanupMutex.Lock()
	defer app.directoryCleanupMutex.Unlock()
	app.activeURLWorkers--
	if app.activeURLWorkers == 0 {
		app.cleanupPendingDirectories()
	}
}

func (app *application) cleanup(downloadsDir string) {
	if downloadsDir == "" {
		return
	}
	directory, err := filepath.Abs(downloadsDir)
	if err != nil {
		return
	}
	app.directoryCleanupMutex.Lock()
	defer app.directoryCleanupMutex.Unlock()
	if app.pendingDirectories == nil {
		app.pendingDirectories = make(map[string]struct{})
	}
	app.pendingDirectories[directory] = struct{}{}
	if app.activeURLWorkers == 0 {
		app.cleanupPendingDirectories()
	}
}

// Hold directoryCleanupMutex through admission and removal: a new URL must not
// start using a directory between the last-worker check and the final rmdir.
func (app *application) cleanupPendingDirectories() {
	if len(app.pendingDirectories) == 0 {
		return
	}
	directories := make([]string, 0, len(app.pendingDirectories))
	for directory := range app.pendingDirectories {
		directories = append(directories, directory)
	}
	app.pendingDirectories = nil
	root, err := filepath.Abs(app.config.DownloadsDirectory)
	if err != nil {
		return
	}
	rootInfo, _ := os.Stat(root)
	// Remove descendants first so requested empty parents can be removed too.
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if directory == root {
			continue
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || rootInfo != nil && os.SameFile(info, rootInfo) {
			continue
		}
		// Never recursively delete or remove files/symlinks passed as directories.
		_ = os.Remove(directory)
	}
}
