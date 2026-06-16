package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetStreamSegmentsReturnsErrorWhenSegmentKeyIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10,
segment.ts
#EXT-X-ENDLIST
`))
	}))
	defer server.Close()

	segments, key, err := getStreamSegments(server.URL + "/playlist.m3u8")
	if err == nil {
		t.Fatal("getStreamSegments() error = nil, want missing key error")
	}
	if segments != nil || key != nil {
		t.Fatalf("getStreamSegments() = (%v, %v, %v), want nil segments and nil key on error", segments, key, err)
	}
	if !strings.Contains(err.Error(), "encryption key") {
		t.Fatalf("getStreamSegments() error = %q, want encryption key context", err)
	}
}
