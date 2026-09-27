package beatport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetPlaylistRetriesRequestTimeout(t *testing.T) {
	attempts := 0
	client := &http.Client{
		Timeout: 10 * time.Millisecond,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":10753356,"name":"Test Playlist"}`)),
				Request:    req,
			}, nil
		}),
	}
	inst := authenticatedTestClient(client)

	playlist, err := inst.GetPlaylist(10753356)
	if err != nil {
		t.Fatalf("GetPlaylist() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("GetPlaylist() attempts = %d, want 2", attempts)
	}
	if playlist.ID != 10753356 {
		t.Fatalf("GetPlaylist() ID = %d, want 10753356", playlist.ID)
	}
}

func TestFetchDoesNotRetryPostTimeout(t *testing.T) {
	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, context.DeadlineExceeded
		}),
	}
	inst := authenticatedTestClient(client)

	_, err := inst.fetch(http.MethodPost, tokenEndpoint, map[string]string{"grant_type": "refresh_token"}, "application/x-www-form-urlencoded")
	if err == nil {
		t.Fatal("fetch() error = nil, want timeout")
	}
	if attempts != 1 {
		t.Fatalf("fetch() attempts = %d, want 1", attempts)
	}
}

func authenticatedTestClient(client *http.Client) *Beatport {
	return &Beatport{
		store:   StoreBeatport,
		client:  client,
		headers: map[string]string{},
		auth: &Auth{tokenPair: &tokenPair{
			AccessToken: "test-token",
			IssuedAt:    time.Now().Unix(),
			ExpiresIn:   3600,
		}},
	}
}
