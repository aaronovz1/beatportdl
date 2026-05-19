package beatport

import "testing"

func TestParseUrlTrack(t *testing.T) {
	link, err := (&Beatport{}).ParseUrl("https://www.beatport.com/track/strobe/1696999")
	if err != nil {
		t.Fatalf("ParseUrl() error = %v", err)
	}

	if link.Store != StoreBeatport {
		t.Fatalf("Store = %q, want %q", link.Store, StoreBeatport)
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

func TestParseUrlBeatsourceRelease(t *testing.T) {
	link, err := (&Beatport{}).ParseUrl("https://www.beatsource.com/release/example/42")
	if err != nil {
		t.Fatalf("ParseUrl() error = %v", err)
	}

	if link.Store != StoreBeatsource {
		t.Fatalf("Store = %q, want %q", link.Store, StoreBeatsource)
	}
	if link.Type != ReleaseLink {
		t.Fatalf("Type = %q, want %q", link.Type, ReleaseLink)
	}
	if link.ID != 42 {
		t.Fatalf("ID = %d, want 42", link.ID)
	}
}
