package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type segmentRoundTripFunc func(*http.Request) (*http.Response, error)

func (f segmentRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackedSegmentBody struct {
	io.Reader
	closed bool
}

func (b *trackedSegmentBody) Close() error {
	b.closed = true
	return nil
}

func TestDownloadSegmentsClosesBodyBeforeNextRequest(t *testing.T) {
	key := StreamKey{Value: bytes.Repeat([]byte{17}, 16), IV: bytes.Repeat([]byte{34}, 16)}
	plaintext := []byte("audio segment")
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key.IV).CryptBlocks(encrypted, padded)

	var previous *trackedSegmentBody
	client := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = client })
	http.DefaultClient = &http.Client{Transport: segmentRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if previous != nil && !previous.closed {
			return nil, fmt.Errorf("previous segment response body still open")
		}
		previous = &trackedSegmentBody{Reader: bytes.NewReader(encrypted)}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       previous,
			Request:    req,
		}, nil
	})}

	app := &application{}
	path, err := app.downloadSegments(t.TempDir(), []string{"https://example.test/1", "https://example.test/2"}, key, "")
	if err != nil {
		t.Fatalf("downloadSegments(): %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytes.Repeat(plaintext, 2); !bytes.Equal(got, want) {
		t.Fatalf("decrypted output = %q, want %q", got, want)
	}
	if previous == nil || !previous.closed {
		t.Fatal("final segment response body was not closed")
	}
}

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
