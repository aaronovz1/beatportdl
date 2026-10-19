package main

import (
	"sync"
	"sync/atomic"
	"unspok3n/beatportdl/internal/beatport"
)

type catalogTrackSource struct {
	fetchPage    func(int64, int, string) (*beatport.Paginated[beatport.Track], error)
	fetchTrack   func(int64) (*beatport.Track, error)
	fetchRelease func(int64) (*beatport.Release, error)
}

type catalogRelease struct {
	once      sync.Once
	release   *beatport.Release
	directory string
	cover     *trackCover
	err       error
	processed atomic.Bool
}

func (app *application) handleCatalogTracks(inst *beatport.Beatport, link *beatport.Link, downloadsDir string, source catalogTrackSource) error {
	releaseDirectories := app.config.ForceReleaseDirectories
	var wg sync.WaitGroup
	var releasesMutex sync.Mutex
	releases := make(map[int64]*catalogRelease)
	err := ForPaginated[beatport.Track](link.ID, link.Params, source.fetchPage, func(track beatport.Track, _ int) error {
		app.downloadWorker(&wg, func() {
			trackURL := track.StoreUrl()
			fullTrack, err := source.fetchTrack(track.ID)
			if err != nil {
				app.errorLogWrapper(trackURL, "fetch full track", err)
				return
			}
			releaseID := fullTrack.Release.ID
			if releaseID == 0 {
				releaseID = track.Release.ID
			}
			releasesMutex.Lock()
			state := releases[releaseID]
			if state == nil {
				state = &catalogRelease{}
				releases[releaseID] = state
			}
			releasesMutex.Unlock()

			state.once.Do(func() {
				state.release, state.err = source.fetchRelease(releaseID)
				if state.err != nil {
					return
				}
				state.directory = downloadsDir
				if releaseDirectories {
					state.directory, state.err = app.setupDownloadsDirectory(downloadsDir, state.release)
				}
				if state.err == nil {
					state.cover = newTrackCover(app, state.release.Image, state.directory, app.requireCover(true, releaseDirectories))
				}
			})
			if state.err != nil {
				app.errorLogWrapper(trackURL, "prepare track release", state.err)
				return
			}
			fullTrack.Release = *state.release
			processed, err := app.handleTrack(inst, fullTrack, state.directory, state.cover)
			if err != nil {
				app.errorLogWrapper(trackURL, "handle track", err)
				return
			}
			if processed {
				state.processed.Store(true)
			}
		})
		return nil
	})

	// Pagination can fail while workers still use shared covers and directories.
	wg.Wait()
	for _, state := range releases {
		if releaseDirectories && state.processed.Load() {
			if err := state.cover.Handle(); err != nil {
				app.errorLogWrapper(state.release.StoreUrl(), "handle cover file", err)
			}
		}
		state.cover.CleanupTemp()
		if state.directory != "" {
			app.cleanup(state.directory)
		}
	}
	return err
}
