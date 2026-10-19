package beatport

import (
	"strings"
	"testing"
)

func TestParseUrlTrack(t *testing.T) {
	link, err := (&Beatport{}).ParseUrl("https://www.beatport.com/track/strobe/1696999")
	if err != nil {
		t.Fatalf("ParseUrl() error = %v", err)
	}

	if link.Type != TrackLink {
		t.Fatalf("Type = %q, want %q", link.Type, TrackLink)
	}
	if link.ID != 1696999 {
		t.Fatalf("ID = %d, want 1696999", link.ID)
	}
}

func TestParseUrlKeepsLabelQueryParams(t *testing.T) {
	link, err := (&Beatport{}).ParseUrl("https://www.beatport.com/label/example/177/tracks?genre_id=12&order_by=-publish_date")
	if err != nil {
		t.Fatalf("ParseUrl() error = %v", err)
	}

	if link.Type != LabelLink {
		t.Fatalf("Type = %q, want %q", link.Type, LabelLink)
	}
	if link.ID != 177 {
		t.Fatalf("ID = %d, want 177", link.ID)
	}
	if link.Params != "genre_id=12&order_by=-publish_date" {
		t.Fatalf("Params = %q", link.Params)
	}
}

func TestParseUrlRejectsRemovedBeatsource(t *testing.T) {
	for _, rawURL := range []string{
		"https://www.beatsource.com/release/example/42",
		"https://api.beatsource.com/v4/catalog/tracks/42/",
		"https://beatsource.com/track/example/42",
		"https://WWW.BEATSOURCE.COM/track/example/42",
	} {
		link, err := (&Beatport{}).ParseUrl(rawURL)
		if link != nil || err == nil || !strings.Contains(err.Error(), "Beatsource support has been removed") {
			t.Errorf("ParseUrl(%q) = %+v, %v; want migration guidance and no link", rawURL, link, err)
		}
	}
}

func TestParseUrlRejectsIncompletePaths(t *testing.T) {
	for _, path := range []string{
		"", "/", "/en", "/catalog", "/library", "/library/",
		"/en/library", "/en/catalog", "/en/catalog/", "/en/catalog/library",
		"/library/playlists", "/library/playlist", "/library/unknown",
		"/en/catalog/library/playlists", "/en/catalog/tracks", "/en/catalog/releases",
		"/track", "/track/example", "/release/example", "/playlists/example",
		"/chart/example", "/playlist/example", "/label/example", "/artist/example",
		"/tracks", "/releases", "/track/example/not-an-id",
	} {
		t.Run(path, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("ParseUrl(%q) panicked: %v", path, recovered)
				}
			}()
			link, err := (&Beatport{}).ParseUrl("https://www.beatport.com" + path)
			if link != nil || err == nil {
				t.Fatalf("ParseUrl(%q) = %+v, %v; want error and no link", path, link, err)
			}
		})
	}
}

func TestParseUrlSupportedRoutes(t *testing.T) {
	for _, tc := range []struct {
		url      string
		linkType LinkType
		id       int64
		params   string
	}{
		{"https://www.beatport.com/track/example/11", TrackLink, 11, ""},
		{"https://www.beatport.com/release/example/12", ReleaseLink, 12, ""},
		{"https://www.beatport.com/chart/example/13", ChartLink, 13, ""},
		{"https://www.beatport.com/playlist/example/14", ChartLink, 14, ""},
		{"https://www.beatport.com/playlists/example/15", PlaylistLink, 15, ""},
		{"https://www.beatport.com/label/example/16/tracks?genre_id=12&order_by=-publish_date", LabelLink, 16, "genre_id=12&order_by=-publish_date"},
		{"https://www.beatport.com/artist/example/17", ArtistLink, 17, ""},
		{"https://www.beatport.com/library/playlists/18", PlaylistLink, 18, ""},
		{"https://www.beatport.com/library/playlist/19?foo=one%20two", PlaylistLink, 19, "foo=one%20two"},
		{"https://www.beatport.com/en/track/example/20", TrackLink, 20, ""},
		{"https://www.beatport.com/en/catalog/tracks/21/", TrackLink, 21, ""},
		{"https://www.beatport.com/es/catalog/releases/22/?page=2", ReleaseLink, 22, "page=2"},
		{"https://www.beatport.com/en/catalog/library/playlists/23", PlaylistLink, 23, ""},
		{"https://api.beatport.com/v4/catalog/tracks/24/", TrackLink, 24, ""},
		{"https://api.beatport.com/v4/catalog/releases/25/?foo=bar", ReleaseLink, 25, "foo=bar"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			link, err := (&Beatport{}).ParseUrl(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if link.Original != tc.url || link.Type != tc.linkType || link.ID != tc.id || link.Params != tc.params {
				t.Fatalf("ParseUrl() = %+v; want original=%q type=%q id=%d params=%q", link, tc.url, tc.linkType, tc.id, tc.params)
			}
		})
	}
}
