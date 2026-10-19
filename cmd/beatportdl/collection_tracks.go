package main

import (
	"fmt"
	"sync"
	"unspok3n/beatportdl/internal/beatport"
)

type collectionRelease struct {
	once    sync.Once
	release *beatport.Release
	err     error
}

type collectionReleases struct {
	mu          sync.Mutex
	entries     map[int64]*collectionRelease
	directories map[string]struct{}
}

func (cache *collectionReleases) get(inst *beatport.Beatport, id int64) (*beatport.Release, error) {
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[int64]*collectionRelease)
	}
	entry := cache.entries[id]
	if entry == nil {
		entry = &collectionRelease{}
		cache.entries[id] = entry
	}
	cache.mu.Unlock()
	entry.once.Do(func() { entry.release, entry.err = inst.GetRelease(id) })
	return entry.release, entry.err
}

func (cache *collectionReleases) rememberDirectory(directory string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.directories == nil {
		cache.directories = make(map[string]struct{})
	}
	cache.directories[directory] = struct{}{}
}

// Collection pagination joins every worker before directory cleanup may run.
func (cache *collectionReleases) cleanup(app *application) {
	for directory := range cache.directories {
		app.cleanup(directory)
	}
}

func (app *application) skipCollectionTrack(track *beatport.Track, directory string, media *trackMedia) bool {
	if app.config.TrackExists != "skip" {
		return false
	}
	app.activeFilesMutex.Lock()
	defer app.activeFilesMutex.Unlock()
	_, exists := app.findExistingTrackFileByIdentity(directory, media.extension, track.ID, track.ISRC)
	return exists
}

func (app *application) handleCollectionTrack(inst *beatport.Beatport, track *beatport.Track, downloadsDir string, releases *collectionReleases) (bool, error) {
	releaseDirectories := app.config.SortByContext && app.config.ForceReleaseDirectories
	media, err := app.fetchTrackMedia(inst, track.ID, app.config.Quality)
	if err != nil {
		return false, fmt.Errorf("fetch track media: %w", err)
	}
	if !releaseDirectories && app.skipCollectionTrack(track, downloadsDir, media) {
		return false, nil
	}

	release, err := releases.get(inst, track.Release.ID)
	if err != nil {
		return false, fmt.Errorf("fetch track release: %w", err)
	}
	track.Release = *release
	trackDownloadsDir := downloadsDir
	if releaseDirectories {
		trackDownloadsDir = app.downloadsDirectory(downloadsDir, release)
	}
	if releaseDirectories && app.skipCollectionTrack(track, trackDownloadsDir, media) {
		return false, nil
	}

	fullTrack, err := inst.GetTrack(track.ID)
	if err != nil {
		return false, fmt.Errorf("fetch full track: %w", err)
	}
	track.Number = fullTrack.Number
	if _, err := app.createDirectory(trackDownloadsDir); err != nil {
		return false, fmt.Errorf("setup track release directory: %w", err)
	}
	releases.rememberDirectory(trackDownloadsDir)
	cover := newTrackCover(app, release.Image, trackDownloadsDir, app.requireCover(true, app.config.ForceReleaseDirectories))
	defer cover.CleanupTemp()
	processed, err := app.handleTrackMedia(track, trackDownloadsDir, cover, media)
	if err != nil {
		return false, err
	}
	if app.config.ForceReleaseDirectories && processed {
		if err := cover.Handle(); err != nil {
			return processed, fmt.Errorf("handle track release cover file: %w", err)
		}
	}
	return processed, nil
}
