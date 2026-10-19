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
