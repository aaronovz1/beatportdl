package beatport

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBeatportOnlyClientRequests(t *testing.T) {
	inst := New("", authenticatedTestClient(nil).auth)
	inst.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "api.beatport.com" {
			t.Errorf("unexpected API URL: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
			Body: io.NopCloser(strings.NewReader(`{"id":42,"slug":"example","results":[],"tracks":[],"releases":[]}`))}, nil
	})
	for name, fetch := range map[string]func() error{
		"track":    func() error { _, err := inst.GetTrack(42); return err },
		"release":  func() error { _, err := inst.GetRelease(42); return err },
		"playlist": func() error { _, err := inst.GetPlaylist(42); return err },
		"chart":    func() error { _, err := inst.GetChart(42); return err },
		"label":    func() error { _, err := inst.GetLabel(42); return err },
		"artist":   func() error { _, err := inst.GetArtist(42); return err },
		"search":   func() error { _, err := inst.Search("example"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := fetch(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResourceLinksUseBeatport(t *testing.T) {
	for _, tc := range []struct {
		entity interface{ StoreUrl() string }
		want   string
	}{
		{&Track{ID: 42, Slug: "example"}, "https://www.beatport.com/track/example/42"},
		{&Release{ID: 42, Slug: "example"}, "https://www.beatport.com/release/example/42"},
		{&Label{ID: 42, Slug: "example"}, "https://www.beatport.com/label/example/42"},
	} {
		if got := tc.entity.StoreUrl(); got != tc.want {
			t.Errorf("URL = %q, want %q", got, tc.want)
		}
	}
}
