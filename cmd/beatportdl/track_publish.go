package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"unspok3n/beatportdl/internal/beatport"
)

// Fold conservatively even on case-sensitive volumes: a library may be moved
// between filesystems, or multiple directories may share a case-insensitive parent.
func trackPathKey(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return strings.Map(func(r rune) rune {
		first := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			first = min(first, next)
		}
		return first
	}, filepath.Clean(path))
}

func (app *application) claimTrackPath(directory, name, ext string, track *beatport.Track) (string, func(), error) {
	state := app.trackDirectory(directory)
	identity := "\x00" + strings.ToLower(ext) + "\x00" + strconv.FormatInt(track.ID, 10)
	// Track IDs coalesce overlapping collection entries even if their rendered
	// names differ while a write is in progress. The path also guards ID-less rows.
	for {
		state.Lock()
		if err := app.context().Err(); err != nil {
			state.Unlock()
			return "", nil, err
		}
		var wait chan struct{}
		if track.ID != 0 {
			wait = state.writes[identity]
		}
		if wait == nil {
			path, err := app.reserveTrackPathInDirectory(state, directory, name, ext, track.ID, track.ISRC)
			if path == "" || err != nil {
				state.Unlock()
				return path, nil, err
			}
			key := trackPathKey(path)
			wait = state.writes[key]
			if wait == nil {
				done := make(chan struct{})
				state.writes[key] = done
				if track.ID != 0 {
					state.writes[identity] = done
				}
				state.Unlock()
				return path, func() {
					state.Lock()
					defer state.Unlock()
					delete(state.writes, key)
					if track.ID != 0 {
						delete(state.writes, identity)
					}
					close(done)
				}, nil
			}
		}
		state.Unlock()
		select {
		case <-wait:
		case <-app.context().Done():
			return "", nil, app.context().Err()
		}
	}
}

func (app *application) commitTrackMedia(track *beatport.Track, directory string, cover *trackCover, media *trackMedia) (string, bool, error) {
	if err := app.validateDownloadDirectory(directory); err != nil {
		return "", false, err
	}
	name := track.Filename(beatport.NamingPreferences{
		Template:           app.config.TrackFileTemplate,
		Whitespace:         app.config.WhitespaceCharacter,
		ArtistsLimit:       app.config.ArtistsLimit,
		ArtistsShortForm:   app.config.ArtistsShortForm,
		TrackNumberPadding: app.config.TrackNumberPadding,
		KeySystem:          app.config.KeySystem,
	})
	path, release, err := app.claimTrackPath(directory, name, media.extension, track)
	if err != nil || path == "" {
		return "", false, err
	}
	defer release()
	original, err := inspectDownloadFile(path)
	if err != nil {
		return "", false, err
	}
	if original != nil && app.config.TrackExists != "update" && app.config.TrackExists != "overwrite" {
		_, err := app.handleExistingTrackPath(path)
		return "", false, err
	}
	update := original != nil && app.config.TrackExists == "update"
	if update && !app.config.FixTags {
		return path, false, nil
	}

	stagingDir, err := os.MkdirTemp(directory, ".beatportdl-track-")
	if err != nil {
		return "", false, fmt.Errorf("stage track: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	staged := filepath.Join(stagingDir, "audio"+strings.ToLower(filepath.Ext(path)))
	if update {
		app.infoLogWrapper(track.StoreUrl(), "updating tags")
		if err := copyTrackForUpdate(path, original, staged); err != nil {
			return "", false, fmt.Errorf("stage tag update: %w", err)
		}
	} else if err := app.transferTrackMedia(track, staged, media); err != nil {
		return "", false, fmt.Errorf("save track: %w", err)
	}

	var coverPath string
	if app.trackTagsNeedCover(staged) {
		coverPath, err = cover.Path()
		if err != nil {
			return "", false, fmt.Errorf("download cover: %w", err)
		}
	}
	if err := app.tagTrack(staged, track, coverPath); err != nil {
		return "", false, fmt.Errorf("tag track: %w", err)
	}
	if app.config.FixTags {
		if err := validateAudioFile(staged); err != nil {
			return "", false, fmt.Errorf("validate tagged track: %w", err)
		}
	}
	if update {
		if err := os.Chmod(staged, original.Mode().Perm()); err != nil {
			return "", false, fmt.Errorf("restore track permissions: %w", err)
		}
	}
	if err := app.context().Err(); err != nil {
		return "", false, err
	}

	// Never replace a file first seen after staging began. Updates publish a new
	// inode, so even another hard link to the original is not modified.
	if original == nil {
		err = publishNewTrack(staged, path)
	} else if current, statErr := inspectDownloadFile(path); statErr != nil {
		err = statErr
	} else if !sameTrackSnapshot(original, current) {
		err = fmt.Errorf("original track changed during processing")
	} else {
		err = os.Rename(staged, path)
	}
	if err != nil {
		return "", false, fmt.Errorf("publish track: %w", err)
	}
	app.rememberCompletedTrackFile(path, track)
	return path, !update, nil
}

func sameTrackSnapshot(before, current os.FileInfo) bool {
	return current != nil && os.SameFile(before, current) &&
		before.Size() == current.Size() && before.ModTime().Equal(current.ModTime())
}

func copyTrackForUpdate(path string, original os.FileInfo, staged string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	current, err := source.Stat()
	if err != nil {
		return err
	}
	if !sameTrackSnapshot(original, current) {
		return fmt.Errorf("original track changed before it could be copied")
	}
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, source)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
