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

	app := &application{mediaClient: http.DefaultClient}
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

	app := &application{}
	segments, key, err := app.getStreamSegments(server.URL + "/playlist.m3u8")
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

func TestHLSResponseLimits(t *testing.T) {
	for _, size := range []int{31, 32, 33} {
		data, err := readHLSBody(strings.NewReader(strings.Repeat("x", size)), 32)
		if size <= 32 && (err != nil || len(data) != size) {
			t.Fatalf("size %d: data length %d, error %v", size, len(data), err)
		}
		if size > 32 && (err == nil || data != nil) {
			t.Fatalf("oversized response accepted: %d, %v", len(data), err)
		}
	}
}

func TestMaintainedHLSParserResolvesResourceReferences(t *testing.T) {
	var keyPath string
	app := &application{mediaClient: &http.Client{Transport: segmentRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "0123456789abcdef"
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			body = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key\",IV=0x00000000000000000000000000000000\n#EXTINF:10,\nhttps://cdn.example.test/audio.ts\n#EXT-X-ENDLIST\n"
		} else {
			keyPath = r.URL.String()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}}
	segments, key, err := app.getStreamSegments("https://example.test/hls/playlist.m3u8?signature=fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(*segments) != 1 || (*segments)[0] != "https://cdn.example.test/audio.ts" || keyPath != "https://example.test/key" || len(key.Value) != 16 {
		t.Fatalf("resource resolution failed: %v, %q, %v", segments, keyPath, key)
	}
}
