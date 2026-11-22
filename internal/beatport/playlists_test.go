package beatport

import (
	"strings"
	"testing"
)

func TestPlaylistDirectoryHandlesIncompleteBPMRange(t *testing.T) {
	low, high := 120, 128
	for _, tc := range []struct {
		name string
		bpm  []*int
		want string
	}{
		{"empty", nil, ""},
		{"one value", []*int{&low}, ""},
		{"missing endpoint", []*int{&low, nil}, ""},
		{"complete", []*int{&low, &high}, "120-128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Playlist{Name: "Collection", Genres: []string{"Techno"}, BPMRange: tc.bpm}
			prefs := NamingPreferences{Template: "{first_genre}/{name}/BPM {bpm_range}"}
			for _, name := range []string{p.DirectoryName(prefs), p.DirectoryNameWithFirstTrackGenre(prefs, "House")} {
				_, got, ok := strings.Cut(name, "BPM")
				if !ok || strings.TrimSpace(got) != tc.want {
					t.Errorf("directory = %q; want BPM range %q", name, tc.want)
				}
			}
		})
	}
}
