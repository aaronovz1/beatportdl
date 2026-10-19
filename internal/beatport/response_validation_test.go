package beatport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type metadataGetter struct {
	name  string
	fetch func(*Beatport) (bool, error)
}

func metadataGetters() []metadataGetter {
	return []metadataGetter{
		{"track", func(b *Beatport) (bool, error) { value, err := b.GetTrack(42); return value != nil, err }},
		{"release", func(b *Beatport) (bool, error) { value, err := b.GetRelease(42); return value != nil, err }},
		{"playlist", func(b *Beatport) (bool, error) { value, err := b.GetPlaylist(42); return value != nil, err }},
		{"chart", func(b *Beatport) (bool, error) { value, err := b.GetChart(42); return value != nil, err }},
		{"label", func(b *Beatport) (bool, error) { value, err := b.GetLabel(42); return value != nil, err }},
		{"artist", func(b *Beatport) (bool, error) { value, err := b.GetArtist(42); return value != nil, err }},
	}
}

func pageGetters() []metadataGetter {
	return []metadataGetter{
		{"label tracks", func(b *Beatport) (bool, error) { value, err := b.GetLabelTracks(42, 1, ""); return value != nil, err }},
		{"artist tracks", func(b *Beatport) (bool, error) { value, err := b.GetArtistTracks(42, 1, ""); return value != nil, err }},
		{"release tracks", func(b *Beatport) (bool, error) { value, err := b.GetReleaseTracks(42, 1, ""); return value != nil, err }},
		{"chart tracks", func(b *Beatport) (bool, error) { value, err := b.GetChartTracks(42, 1, ""); return value != nil, err }},
		{"label releases", func(b *Beatport) (bool, error) { value, err := b.GetLabelReleases(42, 1, ""); return value != nil, err }},
		{"playlist items", func(b *Beatport) (bool, error) { value, err := b.GetPlaylistItems(42, 1, ""); return value != nil, err }},
	}
}

func responseTestClient(payload string, status int) (*Beatport, *catalogResponseBody) {
	body := &catalogResponseBody{Reader: strings.NewReader(payload)}
	client := authenticatedTestClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: req}, nil
	})})
	return client, body
}

func TestMetadataGettersRejectMalformedSuccess(t *testing.T) {
	for _, getter := range metadataGetters() {
		t.Run(getter.name, func(t *testing.T) {
			for _, payload := range []string{
				`null`, `{}`, `{"detail":"unexpected response"}`, `[]`, `true`,
				`{"id":0}`, `{"id":-1}`, `{"id":null}`, `{"id":"42"}`,
				`{"id":42} {}`, `{"id":42} trailing`,
			} {
				t.Run(payload, func(t *testing.T) {
					client, body := responseTestClient(payload, http.StatusOK)
					present, err := getter.fetch(client)
					if !errors.Is(err, ErrInvalidResponse) || present {
						t.Errorf("malformed success returned resource=%v, err=%v", present, err)
					}
					if !body.closed {
						t.Error("response body was not closed")
					}
				})
			}
		})
	}
}

func TestMetadataGettersAcceptValidResourceAndUnknownFields(t *testing.T) {
	for _, getter := range metadataGetters() {
		t.Run(getter.name, func(t *testing.T) {
			client, body := responseTestClient(" {\"id\":42,\"future_field\":{\"enabled\":true}} \n\t", http.StatusOK)
			present, err := getter.fetch(client)
			if !present || err != nil || !body.closed {
				t.Fatalf("valid response: resource=%v, err=%v, closed=%v", present, err, body.closed)
			}
		})
	}
}

func TestPageGettersRejectMalformedSuccess(t *testing.T) {
	for _, getter := range pageGetters() {
		t.Run(getter.name, func(t *testing.T) {
			for _, payload := range []string{
				`null`, `{}`, `{"detail":"unexpected response"}`, `[]`, `{"next":null}`,
				`{"results":null}`, `{"results":{}}`, `{"results":"none"}`,
				`{"results":[],"next":42}`, `{"results":[],"next":""}`, `{"results":[],"previous":" "}`,
				`{"results":[],"count":-1}`, `{"results":[],"per_page":-1}`,
				`{"results":[],"count":null}`, `{"results":[],"per_page":null}`, `{"results":[],"page":null}`,
				`{"results":[],"count":null,"per_page":null,"page":null}`,
				`{"results":[],"count":false}`, `{"results":[],"per_page":"100"}`, `{"results":[],"page":{}}`,
				`{"results":[{}]}`, `{"results":[null]}`, `{"results":[{"id":-1,"track":{"id":-1}}]}`,
				`{"results":[]} {}`, `{"results":[]} trailing`,
			} {
				t.Run(payload, func(t *testing.T) {
					client, body := responseTestClient(payload, http.StatusOK)
					present, err := getter.fetch(client)
					if !errors.Is(err, ErrInvalidResponse) || present {
						t.Errorf("malformed page returned resource=%v, err=%v", present, err)
					}
					if !body.closed {
						t.Error("response body was not closed")
					}
				})
			}
		})
	}
}

func TestPageGettersAcceptExplicitEmptyAndPopulatedPages(t *testing.T) {
	for _, getter := range pageGetters() {
		t.Run(getter.name, func(t *testing.T) {
			for _, payload := range []string{
				`{"results":[]}`,
				`{"count":0,"next":null,"previous":null,"page":"1","per_page":100,"results":[],"future_field":true}`,
				`{"results":[{"id":42,"track":{"id":101}}],"next":"?page=2","previous":null}`,
			} {
				client, body := responseTestClient(payload, http.StatusOK)
				present, err := getter.fetch(client)
				if !present || err != nil || !body.closed {
					t.Errorf("valid page %s: resource=%v, err=%v, closed=%v", payload, present, err, body.closed)
				}
			}
		})
	}
}

func TestPlaylistItemsRequireTrackIdentityNotRowIdentity(t *testing.T) {
	for _, tc := range []struct {
		payload string
		valid   bool
	}{
		{`{"results":[{"track":{"id":101}}]}`, true},
		{`{"results":[{"id":42}]}`, false},
		{`{"results":[{"id":42,"track":null}]}`, false},
		{`{"results":[{"id":42,"track":{"id":0}}]}`, false},
	} {
		client, body := responseTestClient(tc.payload, http.StatusOK)
		page, err := client.GetPlaylistItems(42, 1, "")
		if tc.valid {
			if err != nil || page == nil || len(page.Results) != 1 || page.Results[0].Track.ID != 101 {
				t.Errorf("valid playlist item: page=%+v, err=%v", page, err)
			}
		} else if err == nil || page != nil {
			t.Errorf("invalid playlist item returned page=%+v, err=%v", page, err)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
	}
}

func TestMetadataGettersPreserveAPIErrors(t *testing.T) {
	getters := append(metadataGetters(), pageGetters()...)
	for _, getter := range getters {
		t.Run(getter.name, func(t *testing.T) {
			client, body := responseTestClient(`{"detail":"subscription required"}`, http.StatusForbidden)
			present, err := getter.fetch(client)
			var apiErr *APIError
			if present || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden || !body.closed {
				t.Fatalf("HTTP error changed: resource=%v, err=%v, closed=%v", present, err, body.closed)
			}
		})
	}
}

func TestCatalogPagePreservesResultsAndPagination(t *testing.T) {
	client, _ := responseTestClient(`{
		"count":5,"next":"?page=3","previous":"?page=1","page":"2","per_page":2,
		"results":[{"id":101,"name":"First"},{"id":102,"name":"Second"}],"future_field":true
	}`, http.StatusOK)
	page, err := client.GetLabelTracks(42, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 2 || page.Results[0].ID != 101 || page.Results[0].Name != "First" || page.Results[1].ID != 102 || page.Results[1].Name != "Second" {
		t.Fatalf("page results changed: %+v", page.Results)
	}
	if page.Count != 5 || page.Page != "2" || page.PerPage != 2 || page.Next == nil || *page.Next != "?page=3" || page.Previous == nil || *page.Previous != "?page=1" {
		t.Fatalf("pagination metadata changed: %+v", page)
	}
}

type responseReadError struct{ err error }

func (r responseReadError) Read([]byte) (int, error) { return 0, r.err }

func TestCatalogDecodePreservesReadErrorsAndClosesBody(t *testing.T) {
	for _, getter := range append(metadataGetters(), pageGetters()...) {
		t.Run(getter.name, func(t *testing.T) {
			for _, readErr := range []error{context.Canceled, io.ErrUnexpectedEOF} {
				client, body := responseTestClient("", http.StatusOK)
				body.Reader = io.MultiReader(strings.NewReader(`{"id":42,"results":[]}`), responseReadError{err: readErr})
				present, err := getter.fetch(client)
				if present || !errors.Is(err, readErr) || !body.closed {
					t.Errorf("read error lost: resource=%v, err=%v, closed=%v; want %v", present, err, body.closed, readErr)
				}
			}
		})
	}
}
