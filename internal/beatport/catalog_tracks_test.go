package beatport

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCatalogTrackRequestsPreserveFiltersAndStore(t *testing.T) {
	for _, store := range []Store{StoreBeatport, StoreBeatsource} {
		for _, scope := range []string{"label_id", "artist_id"} {
			t.Run(string(store)+"/"+scope, func(t *testing.T) {
				inst := authenticatedTestClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					wantHost := "api.beatport.com"
					if store == StoreBeatsource {
						wantHost = "api.beatsource.com"
					}
					if req.URL.Host != wantHost || req.URL.Path != "/v4/catalog/tracks/" {
						t.Errorf("unexpected endpoint: %s", req.URL)
					}
					query := req.URL.Query()
					if len(query[scope]) != 1 || query.Get(scope) != "42" || len(query["page"]) != 1 || query.Get("page") != "3" {
						t.Errorf("incorrect scope/pagination: %v", query)
					}
					if query.Get("genre_name") != "Minimal / Deep Tech" || query.Get("publish_date") != "2026-01-01:2026-10-14" || query.Get("order_by") != "-publish_date" {
						t.Errorf("lost filters: %v", query)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
						Body: io.NopCloser(strings.NewReader(`{"next":null,"results":[{"id":101}]}`)), Request: req}, nil
				})})
				inst.store = store
				fetch := inst.GetLabelTracks
				if scope == "artist_id" {
					fetch = inst.GetArtistTracks
				}
				page, err := fetch(42, 3, "genre_name=Minimal+%2F+Deep+Tech&publish_date=2026-01-01%3A2026-10-14&order_by=-publish_date&"+scope+"=999&page=99")
				if err != nil {
					t.Fatal(err)
				}
				if page.Next != nil || len(page.Results) != 1 || page.Results[0].Store != store {
					t.Fatalf("incorrect response or track store: %+v", page)
				}
			})
		}
	}
}

type catalogResponseBody struct {
	io.Reader
	closed bool
}

func (b *catalogResponseBody) Close() error { b.closed = true; return nil }

func TestCatalogTracksErrorsCloseResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"detail":"subscription required"}`},
		{"malformed response", http.StatusOK, `{"results":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &catalogResponseBody{Reader: strings.NewReader(tc.body)}
			inst := authenticatedTestClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: body, Request: req}, nil
			})})
			_, err := inst.GetLabelTracks(42, 1, "")
			if err == nil || !body.closed {
				t.Fatalf("error = %v, response closed = %v", err, body.closed)
			}
			if tc.status == http.StatusForbidden {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
					t.Fatalf("error = %v, want APIError", err)
				}
			}
		})
	}
}

func TestCatalogTracksRejectsMalformedFiltersBeforeRequest(t *testing.T) {
	inst := authenticatedTestClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Error("malformed filters should not trigger a request")
		return nil, errors.New("unexpected request")
	})})
	if _, err := inst.GetLabelTracks(42, 1, "genre_name=%zz"); err == nil {
		t.Fatal("expected invalid filter error")
	}
}

func TestGetArtistTracksUsesScopedCatalogEndpoint(t *testing.T) {
	inst := authenticatedTestClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/v4/catalog/tracks/" {
			t.Errorf("request = %s %s, want GET /v4/catalog/tracks/", req.Method, req.URL.Path)
		}
		query := req.URL.Query()
		for key, want := range map[string]string{"artist_id": "42", "page": "2", "genre_name": "bass house", "per_page": "100"} {
			if got := query[key]; len(got) != 1 || got[0] != want {
				t.Errorf("query[%q] = %v, want [%s]", key, got, want)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"next":"page3","results":[{"id":101,"release":{"id":9}}]}`)), Request: req}, nil
	})})
	page, err := inst.GetArtistTracks(42, 2, "genre_name=bass+house&per_page=100&artist_id=999&page=8")
	if err != nil {
		t.Fatal(err)
	}
	if page.Next == nil || len(page.Results) != 1 || page.Results[0].Store != StoreBeatport || page.Results[0].Release.ID != 9 {
		t.Fatalf("unexpected catalog page: %+v", page)
	}
}
