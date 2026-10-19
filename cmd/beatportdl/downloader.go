package main

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
	"unspok3n/beatportdl/internal/taglib"
)

func (app *application) errorLogWrapper(url, step string, err error) {
	app.LogError(fmt.Sprintf("[%s] %s", url, step), err)
}

func (app *application) infoLogWrapper(url, message string) {
	app.LogInfo(fmt.Sprintf("[%s] %s", url, message))
}

func (app *application) createDirectory(baseDir string, subDir ...string) (string, error) {
	fullPath := filepath.Join(baseDir, filepath.Join(subDir...))
	err := CreateDirectory(fullPath)
	return fullPath, err
}

type DownloadsDirectoryEntity interface {
	DirectoryName(n beatport.NamingPreferences) string
}

// FirstTrackGenreProvider interface for entities that can provide the first track's genre
type FirstTrackGenreProvider interface {
	DirectoryNameWithFirstTrackGenre(n beatport.NamingPreferences, firstTrackGenre string) string
}

// getFirstTrackGenreFromPlaylist fetches the first track from a playlist and returns its genre
func (app *application) getFirstTrackGenreFromPlaylist(inst *beatport.Beatport, playlistID int64) (string, error) {
	// Get the first page of playlist items
	items, err := inst.GetPlaylistItems(playlistID, 1, "")
	if err != nil {
		return "", err
	}

	if len(items.Results) == 0 {
		return "", fmt.Errorf("playlist is empty")
	}

	// Get the first track's full details
	firstTrack, err := inst.GetTrack(items.Results[0].Track.ID)
	if err != nil {
		return "", err
	}

	return firstTrack.Genre.Name, nil
}

// getFirstTrackGenreFromChart fetches the first track from a chart and returns its genre
func (app *application) getFirstTrackGenreFromChart(inst *beatport.Beatport, chartID int64) (string, error) {
	// Get the first page of chart tracks
	tracks, err := inst.GetChartTracks(chartID, 1, "")
	if err != nil {
		return "", err
	}

	if len(tracks.Results) == 0 {
		return "", fmt.Errorf("chart is empty")
	}

	// Get the first track's full details
	firstTrack, err := inst.GetTrack(tracks.Results[0].ID)
	if err != nil {
		return "", err
	}

	return firstTrack.Genre.Name, nil
}

func (app *application) downloadsDirectory(baseDir string, entity DownloadsDirectoryEntity) string {
	if app.config.SortByContext {
		var subDir string
		switch castedEntity := entity.(type) {
		case *beatport.Release:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.ReleaseDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					ArtistsLimit:       app.config.ArtistsLimit,
					ArtistsShortForm:   app.config.ArtistsShortForm,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
			if app.config.SortByLabel && entity != nil {
				baseDir = filepath.Join(baseDir, castedEntity.Label.Name)
			}
		case *beatport.Playlist:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.PlaylistDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
		case *beatport.Chart:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.ChartDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
		case *beatport.Label:
			baseDir = filepath.Join(baseDir, "Labels")
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:   app.config.LabelDirectoryTemplate,
					Whitespace: app.config.WhitespaceCharacter,
				},
			)
		case *beatport.Artist:
			baseDir = filepath.Join(baseDir, "Artists")
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:   app.config.ArtistDirectoryTemplate,
					Whitespace: app.config.WhitespaceCharacter,
				},
			)
		}
		baseDir = filepath.Join(baseDir, subDir)
	}
	return baseDir
}

func (app *application) setupDownloadsDirectory(baseDir string, entity DownloadsDirectoryEntity) (string, error) {
	return app.createDirectory(app.downloadsDirectory(baseDir, entity))
}

func (app *application) setupTrackDownloadsDirectory(baseDir string, track *beatport.Track) (string, error) {
	if app.config.SortByContext {
		genre := beatport.SanitizePath(track.Genre.Name, app.config.WhitespaceCharacter)
		baseDir = filepath.Join(baseDir, genre)
	}
	return app.createDirectory(baseDir)
}

// setupDownloadsDirectoryWithFirstTrackGenre sets up the downloads directory using the first track's genre
func (app *application) setupDownloadsDirectoryWithFirstTrackGenre(baseDir string, entity DownloadsDirectoryEntity, provider FirstTrackGenreProvider, inst *beatport.Beatport, entityID int64) (string, error) {
	if app.config.SortByContext {
		var subDir string
		var firstGenre string
		var err error

		// Get the first track's genre based on entity type
		switch entity.(type) {
		case *beatport.Playlist:
			if !strings.Contains(app.config.PlaylistDirectoryTemplate, "{first_genre}") {
				return app.setupDownloadsDirectory(baseDir, entity)
			}
			firstGenre, err = app.getFirstTrackGenreFromPlaylist(inst, entityID)
		case *beatport.Chart:
			if !strings.Contains(app.config.ChartDirectoryTemplate, "{first_genre}") {
				return app.setupDownloadsDirectory(baseDir, entity)
			}
			firstGenre, err = app.getFirstTrackGenreFromChart(inst, entityID)
		default:
			// Fall back to original behavior for other entity types
			return app.setupDownloadsDirectory(baseDir, entity)
		}

		if err != nil {
			// If we can't get the first track's genre, fall back to the original behavior
			return app.setupDownloadsDirectory(baseDir, entity)
		}

		// Use the first track's genre for directory naming
		switch castedEntity := entity.(type) {
		case *beatport.Playlist:
			subDir = castedEntity.DirectoryNameWithFirstTrackGenre(
				beatport.NamingPreferences{
					Template:           app.config.PlaylistDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
				firstGenre,
			)
		case *beatport.Chart:
			subDir = castedEntity.DirectoryNameWithFirstTrackGenre(
				beatport.NamingPreferences{
					Template:           app.config.ChartDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
				firstGenre,
			)
		}
		baseDir = filepath.Join(baseDir, subDir)
	}
	return app.createDirectory(baseDir)
}

func (app *application) requireCover(respectFixTags, respectKeepCover bool) bool {
	fixTags := respectFixTags && app.config.FixTags &&
		(app.config.CoverSize != config.DefaultCoverSize || app.config.Quality != "lossless")
	keepCover := respectKeepCover && app.config.SortByContext && app.config.KeepCover
	return fixTags || keepCover
}

func (app *application) downloadCover(image beatport.Image, downloadsDir string) (string, error) {
	coverUrl := image.FormattedUrl(app.config.CoverSize)
	coverPath := filepath.Join(downloadsDir, uuid.New().String())
	err := app.downloadFile(coverUrl, coverPath, "")
	if err != nil {
		os.Remove(coverPath)
		return "", err
	}
	return coverPath, nil
}

type trackCover struct {
	app          *application
	image        beatport.Image
	downloadsDir string
	download     func() (string, error)
	once         sync.Once
	path         string
	err          error
}

func newTrackCover(app *application, image beatport.Image, downloadsDir string, required bool) *trackCover {
	if !required {
		return nil
	}
	return &trackCover{
		app:          app,
		image:        image,
		downloadsDir: downloadsDir,
	}
}

func (cover *trackCover) Path() (string, error) {
	if cover == nil {
		return "", nil
	}
	cover.once.Do(func() {
		if cover.download != nil {
			cover.path, cover.err = cover.download()
			return
		}
		cover.path, cover.err = cover.app.downloadCover(cover.image, cover.downloadsDir)
	})
	return cover.path, cover.err
}

func (cover *trackCover) CleanupTemp() {
	if cover == nil {
		return
	}
	cleanupCoverTemp(cover.path)
}

func (cover *trackCover) Handle() error {
	if cover == nil {
		return nil
	}
	path, err := cover.Path()
	if err != nil {
		return err
	}
	return cover.app.handleCoverFile(path)
}

func (app *application) handleCoverFile(path string) error {
	if path == "" {
		return nil
	}
	if app.config.KeepCover && app.config.SortByContext {
		newPath := filepath.Dir(path) + "/cover.jpg"
		if err := os.Rename(path, newPath); err != nil {
			return err
		}
	} else {
		os.Remove(path)
	}
	return nil
}

func (app *application) trackTagsNeedCover(location string) bool {
	if !app.config.FixTags {
		return false
	}
	fileExt := filepath.Ext(location)
	return app.config.CoverSize != config.DefaultCoverSize || fileExt == ".m4a"
}

func cleanupCoverTemp(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

var (
	ErrTrackFileExists = errors.New("file already exists")
)

type trackFileIdentity struct {
	TrackID string
	ISRC    string
}

func (identity trackFileIdentity) hasKeys() bool {
	return identity.TrackID != "" || identity.ISRC != ""
}

func (identity trackFileIdentity) matches(trackID int64, isrc string) bool {
	if trackID != 0 && identity.TrackID == strconv.FormatInt(trackID, 10) {
		return true
	}
	return isrc != "" && strings.EqualFold(identity.ISRC, isrc)
}

func (identity trackFileIdentity) keys() []string {
	var keys []string
	if identity.TrackID != "" {
		keys = append(keys, trackIDIdentityKey(identity.TrackID))
	}
	if identity.ISRC != "" {
		keys = append(keys, isrcIdentityKey(identity.ISRC))
	}
	return keys
}

func trackIDIdentityKey(trackID string) string {
	return "track_id:" + trackID
}

func isrcIdentityKey(isrc string) string {
	return "isrc:" + strings.ToUpper(isrc)
}

func (app *application) readExistingTrackFileIdentity(path string) (trackFileIdentity, error) {
	if app.readTrackFileIdentity != nil {
		return app.readTrackFileIdentity(path)
	}

	file, err := taglib.Read(path)
	if err != nil {
		return trackFileIdentity{}, err
	}
	defer file.Close()

	mappingsByFormat := app.config.TagMappings
	if mappingsByFormat == nil {
		mappingsByFormat = config.DefaultTagMappings
	}

	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	mappings := mappingsByFormat[format]
	return trackFileIdentity{
		TrackID: readMappedTagProperty(file, mappings, "track_id"),
		ISRC:    readMappedTagProperty(file, mappings, "track_isrc"),
	}, nil
}

func readMappedTagProperty(file *taglib.File, mappings map[string]string, field string) string {
	property := mappings[field]
	if property == "" {
		return ""
	}
	property = strings.TrimSuffix(property, rawTagSuffix)
	return strings.TrimSpace(file.GetProperty(property))
}

func (app *application) fetchTrackDownload(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error) {
	if app.downloadTrack != nil {
		return app.downloadTrack(inst, id, quality)
	}
	return inst.DownloadTrack(id, quality)
}

func (app *application) fetchTrackStream(inst *beatport.Beatport, id int64) (*beatport.TrackStream, error) {
	if app.streamTrack != nil {
		return app.streamTrack(inst, id)
	}
	return inst.StreamTrack(id)
}

func (app *application) findExistingTrackFileByIdentity(directory, fileExtension string, trackID int64, isrc string) (string, bool) {
	if trackID == 0 && isrc == "" {
		return "", false
	}

	index := app.trackIdentityFilesForDirectory(directory, fileExtension)
	identity := trackFileIdentity{ISRC: isrc}
	if trackID != 0 {
		identity.TrackID = strconv.FormatInt(trackID, 10)
	}
	for _, key := range identity.keys() {
		for _, candidate := range index.byIdentity[key] {
			if owner, active := app.activeFiles[candidate.path]; active && owner != trackID {
				continue
			}
			info, err := os.Stat(candidate.path)
			if err != nil || !info.Mode().IsRegular() {
				index.remove(candidate.path)
				continue
			}
			// Metadata can change in place even when sync tools preserve size and mtime.
			currentIdentity, err := app.readExistingTrackFileIdentity(candidate.path)
			if err != nil {
				index.remove(candidate.path)
				continue
			}
			if !candidate.unchanged(info) || currentIdentity != candidate.diskIdentity {
				index.remove(candidate.path)
				candidate = index.store(candidate.path, currentIdentity, info)
			}
			if candidate.identity.matches(trackID, isrc) {
				return candidate.path, true
			}
		}
	}

	return "", false
}

// The caller holds activeFilesMutex for both reservations and index mutations.
func (app *application) trackIdentityFilesForDirectory(directory, fileExtension string) *trackIdentityIndex {
	directory = filepath.Clean(directory)
	cacheKey := directory + "\x00" + strings.ToLower(fileExtension)
	if app.trackIdentityFiles == nil {
		app.trackIdentityFiles = make(map[string]*trackIdentityIndex)
	}
	if index, ok := app.trackIdentityFiles[cacheKey]; ok {
		return index
	}

	index := &trackIdentityIndex{
		files:      make(map[string]*indexedTrackFile),
		byIdentity: make(map[string]map[string]*indexedTrackFile),
	}
	app.trackIdentityFiles[cacheKey] = index

	entries, err := os.ReadDir(directory)
	if err != nil {
		return index
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), fileExtension) {
			continue
		}

		candidate := filepath.Join(directory, entry.Name())
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		identity, err := app.readExistingTrackFileIdentity(candidate)
		if err != nil {
			continue
		}
		index.store(candidate, identity, info)
	}

	return index
}

func (app *application) resetTrackFileState() {
	app.activeFilesMutex.Lock()
	defer app.activeFilesMutex.Unlock()
	app.activeFiles = make(map[string]int64, len(app.urls))
	app.trackIdentityFiles = nil
}

func (app *application) rememberCompletedTrackFile(path string, track *beatport.Track) {
	app.activeFilesMutex.Lock()
	defer app.activeFilesMutex.Unlock()
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	diskIdentity, err := app.readExistingTrackFileIdentity(path)
	if err != nil {
		return
	}
	identity := trackFileIdentity{ISRC: track.ISRC}
	if track.ID != 0 {
		identity.TrackID = strconv.FormatInt(track.ID, 10)
	}
	index := app.trackIdentityFilesForDirectory(filepath.Dir(path), filepath.Ext(path))
	// A successful transfer is known even when the user disables identity tags.
	file := index.store(path, identity, info)
	file.diskIdentity = diskIdentity
}

func (app *application) handleExistingTrackPath(path string) (string, error) {
	switch app.config.TrackExists {
	case "skip":
		return "", nil
	case "update", "overwrite":
		return path, nil
	case "error":
		return "", ErrTrackFileExists
	}
	return path, nil
}

func (app *application) existingTrackFileBelongsToDifferentTrack(path string, trackID int64, isrc string) bool {
	if trackID == 0 && isrc == "" {
		return false
	}

	identity, err := app.readExistingTrackFileIdentity(path)
	if err != nil || !identity.hasKeys() {
		return false
	}
	return !identity.matches(trackID, isrc)
}

func (app *application) reserveSuffixedTrackFilePath(directory, fileName, fileExtension string, trackID int64) (string, error) {
	for i := 1; ; i++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s (%d)%s", fileName, i, fileExtension))
		if owner, exists := app.activeFiles[candidate]; exists && owner != trackID {
			continue
		}

		if _, err := os.Stat(candidate); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		app.activeFiles[candidate] = trackID
		return candidate, nil
	}
}

func (app *application) reserveTrackFilePath(directory, fileName, fileExtension string, trackID int64, isrc string) (string, error) {
	basePath := filepath.Join(directory, fileName+fileExtension)

	app.activeFilesMutex.Lock()
	defer app.activeFilesMutex.Unlock()

	if existingPath, ok := app.findExistingTrackFileByIdentity(directory, fileExtension, trackID, isrc); ok {
		path, err := app.handleExistingTrackPath(existingPath)
		if path != "" {
			app.activeFiles[path] = trackID
		}
		return path, err
	}

	if owner, exists := app.activeFiles[basePath]; exists && owner != trackID {
		return app.reserveSuffixedTrackFilePath(directory, fileName, fileExtension, trackID)
	}

	if _, err := os.Stat(basePath); err == nil {
		if app.existingTrackFileBelongsToDifferentTrack(basePath, trackID, isrc) {
			return app.reserveSuffixedTrackFilePath(directory, fileName, fileExtension, trackID)
		}

		path, err := app.handleExistingTrackPath(basePath)
		if path != "" {
			app.activeFiles[path] = trackID
		}
		return path, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	app.activeFiles[basePath] = trackID
	return basePath, nil
}

func (app *application) saveTrack(inst *beatport.Beatport, track *beatport.Track, directory string, quality string) (location string, downloaded bool, err error) {
	media, err := app.fetchTrackMedia(inst, track.ID, quality)
	if err != nil {
		return "", false, err
	}
	return app.saveTrackMedia(track, directory, media)
}

func (app *application) saveTrackMedia(track *beatport.Track, directory string, media *trackMedia) (location string, downloaded bool, err error) {
	fileName := track.Filename(
		beatport.NamingPreferences{
			Template:           app.config.TrackFileTemplate,
			Whitespace:         app.config.WhitespaceCharacter,
			ArtistsLimit:       app.config.ArtistsLimit,
			ArtistsShortForm:   app.config.ArtistsShortForm,
			TrackNumberPadding: app.config.TrackNumberPadding,
			KeySystem:          app.config.KeySystem,
		},
	)
	filePath, err := app.reserveTrackFilePath(directory, fileName, media.extension, track.ID, track.ISRC)
	if err != nil {
		return "", false, err
	}
	if filePath == "" {
		return "", false, nil
	}
	if _, err := os.Stat(filePath); err == nil && app.config.TrackExists == "update" {
		app.infoLogWrapper(track.StoreUrl(), "updating tags")
		return filePath, false, nil
	}

	var prefix string
	infoDisplay := fmt.Sprintf("%s (%s) [%s]", track.Name.String(), track.MixName.String(), media.displayQuality)
	if app.config.ShowProgress {
		prefix = infoDisplay
	} else {
		fmt.Println("Downloading " + infoDisplay)
	}

	if media.download != nil {
		if err := app.downloadFile(media.download.Location, filePath, prefix); err != nil {
			return "", false, err
		}
	} else if media.stream != nil {
		segments, key, err := app.getStreamSegments(media.stream.Url)
		if err != nil {
			return "", false, fmt.Errorf("get stream segments: %w", err)
		}
		segmentsFile, err := app.downloadSegments(directory, *segments, *key, prefix)
		if err != nil {
			return "", false, fmt.Errorf("download segments: %w", err)
		}
		defer os.Remove(segmentsFile)
		if err := remuxToM4AContext(app.context(), segmentsFile, filePath); err != nil {
			return "", false, fmt.Errorf("remux to m4a: %w", err)
		}
	}

	if !app.config.ShowProgress {
		fmt.Printf("Finished downloading %s\n", infoDisplay)
	}

	return filePath, true, nil
}

const (
	rawTagSuffix = "_raw"
)

func (app *application) tagTrack(location string, track *beatport.Track, coverPath string) error {
	fileExt := filepath.Ext(location)
	if !app.config.FixTags {
		return nil
	}
	file, err := taglib.Read(location)
	if err != nil {
		return err
	}
	defer file.Close()

	subgenre := ""
	if track.Subgenre != nil {
		subgenre = track.Subgenre.Name
	}
	mappingValues := map[string]string{
		"track_id":       strconv.Itoa(int(track.ID)),
		"track_url":      track.StoreUrl(),
		"track_name":     fmt.Sprintf("%s (%s)", track.Name.String(), track.MixName.String()),
		"track_artists":  track.Artists.Display(0, ""),
		"track_remixers": track.Remixers.Display(0, ""),
		"track_artists_limited": track.Artists.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"track_remixers_limited": track.Remixers.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"track_number":              strconv.Itoa(track.Number),
		"track_number_with_padding": beatport.NumberWithPadding(track.Number, track.Release.TrackCount, app.config.TrackNumberPadding),
		"track_number_with_total":   fmt.Sprintf("%d/%d", track.Number, track.Release.TrackCount),
		"track_genre":               track.Genre.Name,
		"track_subgenre":            subgenre,
		"track_genre_with_subgenre": track.GenreWithSubgenre("|"),
		"track_subgenre_or_genre":   track.SubgenreOrGenre(),
		"track_key":                 track.Key.Display(app.config.KeySystem),
		"track_bpm":                 strconv.Itoa(track.BPM),
		"track_isrc":                track.ISRC,

		"release_id":   strconv.Itoa(int(track.Release.ID)),
		"release_url":  track.Release.StoreUrl(),
		"release_name": track.Release.Name.String(),
		"release_artists": track.Release.Artists.Display(
			0,
			"",
		),
		"release_remixers": track.Release.Remixers.Display(
			0,
			"",
		),
		"release_artists_limited": track.Release.Artists.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"release_remixers_limited": track.Release.Remixers.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"release_date":        track.Release.Date,
		"release_year":        track.Release.Year(),
		"release_track_count": strconv.Itoa(track.Release.TrackCount),
		"release_track_count_with_padding": beatport.NumberWithPadding(
			track.Release.TrackCount, track.Release.TrackCount, app.config.TrackNumberPadding,
		),
		"release_catalog_number": track.Release.CatalogNumber.String(),
		"release_upc":            track.Release.UPC,
		"release_label":          track.Release.Label.Name,
		"release_label_url":      track.Release.Label.StoreUrl(),
	}

	if fileExt == ".m4a" {
		if err = file.StripMp4(); err != nil {
			return err
		}
	} else {
		existingTags, err := file.PropertyKeys()
		if err != nil {
			return fmt.Errorf("read existing tags: %v", err)
		}

		for _, tag := range existingTags {
			file.SetProperty(tag, nil)
		}
	}

	if fileExt == ".flac" {
		for field, property := range app.config.TagMappings["flac"] {
			value := mappingValues[field]
			if value != "" {
				file.SetProperty(property, &value)
			}
		}
	} else if fileExt == ".m4a" {
		rawTags := make(map[string]string)

		for field, property := range app.config.TagMappings["m4a"] {
			if strings.HasSuffix(property, rawTagSuffix) {
				if mappingValues[field] != "" {
					property = strings.TrimSuffix(property, rawTagSuffix)
					rawTags[property] = mappingValues[field]
				}
			} else {
				value := mappingValues[field]
				if value != "" {
					file.SetProperty(property, &value)
				}
			}
		}

		for tag, value := range rawTags {
			file.SetItemMp4(tag, value)
		}
	}

	if coverPath != "" && (app.config.CoverSize != config.DefaultCoverSize || fileExt == ".m4a") {
		data, err := os.ReadFile(coverPath)
		if err != nil {
			return err
		}
		picture := taglib.Picture{
			MimeType:    "image/jpeg",
			PictureType: "Front",
			Description: "Cover",
			Data:        data,
			Size:        uint(len(data)),
		}
		if err := file.SetPicture(&picture); err != nil {
			return err
		}
	}

	if err = file.Save(); err != nil {
		return err
	}

	return nil
}

func (app *application) handleTrack(inst *beatport.Beatport, track *beatport.Track, downloadsDir string, cover *trackCover) (bool, error) {
	media, err := app.fetchTrackMedia(inst, track.ID, app.config.Quality)
	if err != nil {
		return false, fmt.Errorf("save track: %w", err)
	}
	return app.handleTrackMedia(track, downloadsDir, cover, media)
}

func (app *application) handleTrackMedia(track *beatport.Track, downloadsDir string, cover *trackCover, media *trackMedia) (bool, error) {
	location, downloaded, err := app.saveTrackMedia(track, downloadsDir, media)
	if err != nil {
		return false, fmt.Errorf("save track: %w", err)
	}
	if location == "" {
		return false, nil
	}

	coverPath := ""
	if app.trackTagsNeedCover(location) {
		coverPath, err = cover.Path()
		if err != nil {
			return false, fmt.Errorf("download cover: %w", err)
		}
	}

	if err = app.tagTrack(location, track, coverPath); err != nil {
		return false, fmt.Errorf("tag track: %w", err)
	}
	if downloaded || app.config.FixTags {
		app.rememberCompletedTrackFile(location, track)
	}
	return true, nil
}

func forPaginatedWorkers[T any](
	workers *sync.WaitGroup,
	entityId int64,
	params string,
	fetchPage func(id int64, page int, params string) (results *beatport.Paginated[T], err error),
	processItem func(item T, i int) error,
) error {
	// Pagination owns queued workers until every return path has joined them.
	defer workers.Wait()
	page := 1
	for {
		paginated, err := fetchPage(entityId, page, params)
		if err != nil {
			return fmt.Errorf("fetch page: %w", err)
		}

		for i, item := range paginated.Results {
			if err := processItem(item, i); err != nil {
				return fmt.Errorf("process item: %w", err)
			}
		}

		if paginated.Next == nil {
			break
		}
		page++
	}
	return nil
}

func (app *application) handleUrl(url string) {
	link, err := app.bp.ParseUrl(url)
	if err != nil {
		app.errorLogWrapper(url, "parse url", err)
		return
	}

	inst := app.bp

	switch link.Type {
	case beatport.TrackLink:
		app.handleTrackLink(inst, link)
	case beatport.ReleaseLink:
		app.handleReleaseLink(inst, link)
	case beatport.PlaylistLink:
		app.handlePlaylistLink(inst, link)
	case beatport.ChartLink:
		app.handleChartLink(inst, link)
	case beatport.LabelLink:
		app.handleLabelLink(inst, link)
	case beatport.ArtistLink:
		app.handleArtistLink(inst, link)
	default:
		app.LogError("handle URL", ErrUnsupportedLinkType)
	}
}

func (app *application) handleTrackLink(inst *beatport.Beatport, link *beatport.Link) {
	track, err := inst.GetTrack(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch track", err)
		return
	}

	release, err := inst.GetRelease(track.Release.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch track release", err)
		return
	}
	track.Release = *release

	downloadsDir, err := app.setupTrackDownloadsDirectory(app.config.DownloadsDirectory, track)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	wg := sync.WaitGroup{}
	app.downloadWorker(&wg, func() {
		cover := newTrackCover(app, track.Release.Image, downloadsDir, app.requireCover(true, true))
		defer cover.CleanupTemp()

		processed, err := app.handleTrack(inst, track, downloadsDir, cover)
		if err != nil {
			app.errorLogWrapper(link.Original, "handle track", err)
			return
		}

		if !processed {
			return
		}
		if err := cover.Handle(); err != nil {
			app.errorLogWrapper(link.Original, "handle cover file", err)
			return
		}
	})
	wg.Wait()

	app.cleanup(downloadsDir)
}

func (app *application) handleReleaseLink(inst *beatport.Beatport, link *beatport.Link) {
	release, err := inst.GetRelease(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch release", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, release)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	cover := newTrackCover(app, release.Image, downloadsDir, app.requireCover(true, true))
	defer cover.CleanupTemp()

	wg := sync.WaitGroup{}
	var processedTrack atomic.Bool
	for _, trackUrl := range release.TrackUrls {
		app.downloadWorker(&wg, func() {
			trackLink, err := inst.ParseUrl(trackUrl)
			if err != nil {
				app.errorLogWrapper(link.Original, "parse track url", err)
				return
			}

			track, err := inst.GetTrack(trackLink.ID)
			if err != nil {
				app.errorLogWrapper(trackUrl, "fetch release track", err)
				return
			}
			trackStoreUrl := track.StoreUrl()
			track.Release = *release

			processed, err := app.handleTrack(inst, track, downloadsDir, cover)
			if err != nil {
				app.errorLogWrapper(trackStoreUrl, "handle track", err)
				return
			}
			if processed {
				processedTrack.Store(true)
			}
		})
	}
	wg.Wait()

	if !processedTrack.Load() {
		app.cleanup(downloadsDir)
		return
	}
	if err := cover.Handle(); err != nil {
		app.errorLogWrapper(link.Original, "handle cover file", err)
		return
	}

	app.cleanup(downloadsDir)
}

func (app *application) handlePlaylistLink(inst *beatport.Beatport, link *beatport.Link) {
	playlist, err := inst.GetPlaylist(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch playlist", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectoryWithFirstTrackGenre(app.config.DownloadsDirectory, playlist, playlist, inst, link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	wg := sync.WaitGroup{}
	var releases collectionReleases
	defer releases.cleanup(app)
	err = forPaginatedWorkers[beatport.PlaylistItem](&wg, link.ID, "", inst.GetPlaylistItems, func(item beatport.PlaylistItem, i int) error {
		app.downloadWorker(&wg, func() {
			_, err := app.handleCollectionTrack(inst, &item.Track, downloadsDir, &releases)
			if err != nil {
				app.errorLogWrapper(item.Track.StoreUrl(), "handle track", err)
			}
		})
		return nil
	})

	if err != nil {
		app.errorLogWrapper(link.Original, "handle playlist items", err)
		return
	}
}

func (app *application) handleChartLink(inst *beatport.Beatport, link *beatport.Link) {
	chart, err := inst.GetChart(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch chart", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectoryWithFirstTrackGenre(app.config.DownloadsDirectory, chart, chart, inst, link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}
	wg := sync.WaitGroup{}
	var releases collectionReleases
	defer releases.cleanup(app)
	chartCover := newTrackCover(app, chart.Image, downloadsDir, app.requireCover(false, true))
	defer chartCover.CleanupTemp()
	var processedChartTrack atomic.Bool

	err = forPaginatedWorkers[beatport.Track](&wg, link.ID, "", inst.GetChartTracks, func(track beatport.Track, i int) error {
		app.downloadWorker(&wg, func() {
			processed, err := app.handleCollectionTrack(inst, &track, downloadsDir, &releases)
			if processed {
				processedChartTrack.Store(true)
			}
			if err != nil {
				app.errorLogWrapper(track.StoreUrl(), "handle track", err)
				return
			}
		})
		return nil
	})

	if err != nil {
		app.errorLogWrapper(link.Original, "handle playlist items", err)
		return
	}

	if processedChartTrack.Load() {
		if err := chartCover.Handle(); err != nil {
			app.errorLogWrapper(link.Original, "handle cover file", err)
			return
		}
	}
}

func (app *application) handleLabelLink(inst *beatport.Beatport, link *beatport.Link) {
	label, err := inst.GetLabel(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch label", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, label)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	err = app.handleCatalogTracks(inst, link, downloadsDir, catalogTrackSource{
		fetchPage: inst.GetLabelTracks, fetchTrack: inst.GetTrack, fetchRelease: inst.GetRelease,
	})
	if err != nil {
		app.errorLogWrapper(link.Original, "handle label tracks", err)
	}
}

func (app *application) handleArtistLink(inst *beatport.Beatport, link *beatport.Link) {
	artist, err := inst.GetArtist(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch artist", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, artist)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	err = app.handleCatalogTracks(inst, link, downloadsDir, catalogTrackSource{
		fetchPage: inst.GetArtistTracks, fetchTrack: inst.GetTrack, fetchRelease: inst.GetRelease,
	})
	if err != nil {
		app.errorLogWrapper(link.Original, "handle artist tracks", err)
	}
}
