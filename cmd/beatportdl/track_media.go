package main

import (
	"fmt"
	"unspok3n/beatportdl/internal/beatport"
)

// Retain the per-track access check and reuse its result after metadata lookup.
type trackMedia struct {
	extension      string
	displayQuality string
	download       *beatport.TrackDownload
	stream         *beatport.TrackStream
}

func (app *application) fetchTrackMedia(inst *beatport.Beatport, id int64, quality string) (*trackMedia, error) {
	if app.config.Quality == "medium-hls" {
		stream, err := app.fetchTrackStream(inst, id)
		if err != nil {
			return nil, err
		}
		return &trackMedia{extension: ".m4a", displayQuality: "AAC 128kbps - HLS", stream: stream}, nil
	}
	download, err := app.fetchTrackDownload(inst, id, quality)
	if err != nil {
		return nil, err
	}
	media := &trackMedia{download: download}
	switch download.StreamQuality {
	case ".128k.aac.mp4":
		media.extension, media.displayQuality = ".m4a", "AAC 128kbps"
	case ".256k.aac.mp4":
		media.extension, media.displayQuality = ".m4a", "AAC 256kbps"
	case ".flac":
		media.extension, media.displayQuality = ".flac", "FLAC"
	default:
		return nil, fmt.Errorf("invalid stream quality: %s", download.StreamQuality)
	}
	return media, nil
}
