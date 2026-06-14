package beatport

import (
	"testing"
	"time"
)

func TestPlaylistDirectoryNameWithFirstTrackGenre(t *testing.T) {
	playlist := Playlist{
		ID:          99,
		Name:        "Weekend Picks",
		Genres:      []string{"Electronic"},
		TrackCount:  12,
		CreatedDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		UpdatedDate: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	naming := NamingPreferences{
		Template:           "{first_genre} - {name} [{track_count}]",
		TrackNumberPadding: 2,
	}

	got := playlist.DirectoryNameWithFirstTrackGenre(naming, "Afro House")
	want := "Afro House - Weekend Picks [12]"

	if got != want {
		t.Fatalf("DirectoryNameWithFirstTrackGenre() = %q, want %q", got, want)
	}

	fallback := playlist.DirectoryName(naming)
	if fallback != "Electronic - Weekend Picks [12]" {
		t.Fatalf("DirectoryName() = %q, want metadata genre fallback", fallback)
	}
}

func TestPlaylistDirectoryNamePreservesTemplateSubdirectories(t *testing.T) {
	playlist := Playlist{
		ID:          99,
		Name:        "ibiza global radio",
		TrackCount:  12,
		CreatedDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		UpdatedDate: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	naming := NamingPreferences{
		Template:           "{first_genre}/{name}",
		TrackNumberPadding: 2,
	}

	got := playlist.DirectoryNameWithFirstTrackGenre(naming, "Minimal / Deep Tech")
	want := "Minimal ／ Deep Tech/ibiza global radio"

	if got != want {
		t.Fatalf("DirectoryNameWithFirstTrackGenre() = %q, want %q", got, want)
	}
}

func TestChartDirectoryNameWithFirstTrackGenre(t *testing.T) {
	chart := Chart{
		ID:         42,
		Name:       "Top Tracks",
		Slug:       "top-tracks",
		TrackCount: 10,
		Person: ChartPerson{
			OwnerName: "Selector",
		},
		Genres:      []Genre{{Name: "Dance"}},
		AddDate:     time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		ChangeDate:  time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		PublishDate: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
	}
	naming := NamingPreferences{
		Template:           "{first_genre} - {name} [{creator}]",
		TrackNumberPadding: 2,
	}

	got := chart.DirectoryNameWithFirstTrackGenre(naming, "Tech House")
	want := "Tech House - Top Tracks [Selector]"

	if got != want {
		t.Fatalf("DirectoryNameWithFirstTrackGenre() = %q, want %q", got, want)
	}

	fallback := chart.DirectoryName(naming)
	if fallback != "Dance - Top Tracks [Selector]" {
		t.Fatalf("DirectoryName() = %q, want metadata genre fallback", fallback)
	}
}
