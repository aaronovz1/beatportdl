package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMediaRedirectDoesNotSendReferer(t *testing.T) {
	received := make(chan string, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Referer()
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/audio", http.StatusFound)
	}))
	defer origin.Close()

	app := &application{mediaClient: newMediaClient(nil)}
	resp, err := app.getMedia(origin.URL + "/audio?signature=fixture-value")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redirect status = %d", resp.StatusCode)
	}
	if ref := <-received; ref != "" {
		t.Error("media redirect sent a Referer")
	}
}

func TestMediaClientUsesConfiguredProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "media.example.invalid" || !r.URL.IsAbs() {
			t.Errorf("unexpected proxy request: %s", r.URL)
		}
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	app := &application{mediaClient: newMediaClient(proxyURL)}
	for _, path := range []string{"/audio", "/cover", "/manifest.m3u8", "/key", "/segment.ts"} {
		t.Run(path, func(t *testing.T) {
			resp, err := app.getMedia("http://media.example.invalid" + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil || string(data) != path {
				t.Fatalf("expected response from configured proxy, got %q, %v", data, err)
			}
		})
	}
}

func TestMediaClientBoundsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()
	client := newMediaClient(nil)
	client.Timeout = 3 * time.Second
	if _, err := client.Get(server.URL + "/loop"); err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("redirect loop returned %v", err)
	}
}
