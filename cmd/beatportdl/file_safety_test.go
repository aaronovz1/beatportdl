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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unspok3n/beatportdl/internal/beatport"
)

func directDownloadTestApplication(location string) *application {
	app := newTestApplication("overwrite")
	app.logWriter = io.Discard
	app.config.Quality = "lossless"
	app.config.TrackFileTemplate = "{name}"
	app.downloadTrack = func(*beatport.Beatport, int64, string) (*beatport.TrackDownload, error) {
		return &beatport.TrackDownload{Location: location, StreamQuality: ".flac"}, nil
	}
	return app
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("contents of %q = %q, error = %v; want %q", path, got, err, want)
	}
}

func audioFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSaveTrackOverwritePreservesOriginalOnDownloadFailure(t *testing.T) {
	for _, failure := range []string{"status", "short transfer", "empty response"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "status":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "short transfer":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("partial audio"))
				case "empty response":
					w.Header().Set("Content-Length", "0")
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			path := filepath.Join(dir, "Existing.flac")
			if err := os.WriteFile(path, []byte("original good audio"), 0600); err != nil {
				t.Fatal(err)
			}
			app := directDownloadTestApplication(server.URL)
			if _, _, err := app.saveTrack(nil, &beatport.Track{ID: 1, Name: "Existing"}, dir, "lossless"); err == nil {
				t.Error("saveTrack() accepted failed download")
			}
			assertFileContents(t, path, "original good audio")
			assertCatalogFiles(t, dir, "Existing.flac")
		})
	}
}

func TestSaveTrackSuccessfulOverwriteReplacesOriginal(t *testing.T) {
	audio := audioFixture(t, "tone.flac")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(audio)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "Existing.flac")
	if err := os.WriteFile(path, []byte("original audio"), 0600); err != nil {
		t.Fatal(err)
	}
	app := directDownloadTestApplication(server.URL)
	got, downloaded, err := app.saveTrack(nil, &beatport.Track{ID: 1, Name: "Existing"}, dir, "lossless")
	if err != nil || got != path || !downloaded {
		t.Fatalf("saveTrack() = %q, %v; want %q", got, err, path)
	}
	assertFileContents(t, path, string(audio))
	assertCatalogFiles(t, dir, "Existing.flac")
}

func TestDirectDownloadsDoNotRequireFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		quality, streamQuality, extension string
	}{
		{"lossless", ".flac", ".flac"},
		{"medium", ".128k.aac.mp4", ".m4a"},
		{"high", ".256k.aac.mp4", ".m4a"},
	} {
		t.Run(tc.quality, func(t *testing.T) {
			audio := audioFixture(t, "tone"+tc.extension)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(audio) }))
			defer server.Close()
			dir := t.TempDir()
			app := directDownloadTestApplication(server.URL)
			app.config.Quality = tc.quality
			app.downloadTrack = func(*beatport.Beatport, int64, string) (*beatport.TrackDownload, error) {
				return &beatport.TrackDownload{Location: server.URL, StreamQuality: tc.streamQuality}, nil
			}
			processed, err := app.handleTrack(nil, &beatport.Track{ID: 1, Name: "Track"}, dir, nil)
			if !processed || err != nil {
				t.Fatalf("handleTrack() = %v, %v; want true and nil", processed, err)
			}
			assertFileContents(t, filepath.Join(dir, "Track"+tc.extension), string(audio))
			assertCatalogFiles(t, dir, "Track"+tc.extension)
		})
	}
}

func TestDirectDownloadsRejectInvalidAudioBeforePublishing(t *testing.T) {
	for _, extension := range []string{".flac", ".m4a"} {
		for _, existing := range []bool{false, true} {
			for _, payload := range []string{"error page", "truncated audio"} {
				t.Run(fmt.Sprintf("%s/existing=%v/%s", extension, existing, payload), func(t *testing.T) {
					body := []byte("<html>upstream error</html>")
					if payload == "truncated audio" {
						body = audioFixture(t, "tone"+extension)[:16]
					}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
					defer server.Close()
					dir := t.TempDir()
					path := filepath.Join(dir, "Track"+extension)
					if existing {
						if err := os.WriteFile(path, []byte("original good audio"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					app := directDownloadTestApplication(server.URL)
					app.downloadTrack = func(*beatport.Beatport, int64, string) (*beatport.TrackDownload, error) {
						streamQuality := ".flac"
						if extension == ".m4a" {
							streamQuality = ".256k.aac.mp4"
						}
						return &beatport.TrackDownload{Location: server.URL, StreamQuality: streamQuality}, nil
					}
					processed, err := app.handleTrack(nil, &beatport.Track{ID: 1, Name: "Track"}, dir, nil)
					if processed || err == nil {
						t.Error("invalid audio was accepted")
					}
					if existing {
						assertFileContents(t, path, "original good audio")
						assertCatalogFiles(t, dir, "Track"+extension)
					} else {
						assertCatalogFiles(t, dir)
					}
				})
			}
		}
	}
}

func TestHandleTrackRejectsEmptyAudioWithoutPublishing(t *testing.T) {
	for _, unknownLength := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown length=%v", unknownLength), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if unknownLength {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", "0")
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			app := directDownloadTestApplication(server.URL)
			processed, err := app.handleTrack(nil, &beatport.Track{ID: 1, Name: "Empty"}, dir, nil)
			if processed || err == nil {
				t.Fatalf("handleTrack() = %v, %v; want false and error", processed, err)
			}
			assertCatalogFiles(t, dir)
		})
	}
}

func encryptPaddedSegment(t *testing.T, padded []byte, key StreamKey) []byte {
	t.Helper()
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key.IV).CryptBlocks(encrypted, padded)
	return encrypted
}

func TestDecryptSegmentRejectsMalformedDataWithoutPanic(t *testing.T) {
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	for _, tc := range []struct {
		name string
		key  StreamKey
		data []byte
	}{
		{"invalid key", StreamKey{Value: []byte("short"), IV: key.IV}, make([]byte, 16)},
		{"invalid IV", StreamKey{Value: key.Value, IV: []byte("short")}, make([]byte, 16)},
		{"empty ciphertext", key, nil},
		{"unaligned ciphertext", key, make([]byte, 17)},
		{"zero padding", key, encryptPaddedSegment(t, make([]byte, 16), key)},
		{"oversized padding", key, encryptPaddedSegment(t, bytes.Repeat([]byte{17}, 32), key)},
		{"inconsistent padding", key, encryptPaddedSegment(t, append(make([]byte, 15), 2), key)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("decryptSegment() panicked: %v", recovered)
				}
			}()
			if got, err := decryptSegment(tc.data, tc.key); err == nil || got != nil {
				t.Fatalf("decryptSegment() = %x, %v; want nil and error", got, err)
			}
		})
	}
}

func TestDecryptSegmentAcceptsValidPadding(t *testing.T) {
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	for _, plaintext := range [][]byte{[]byte("audio"), bytes.Repeat([]byte{42}, aes.BlockSize)} {
		padding := aes.BlockSize - len(plaintext)%aes.BlockSize
		padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
		got, err := decryptSegment(encryptPaddedSegment(t, padded, key), key)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Fatalf("decryptSegment() = %x, %v; want %x", got, err, plaintext)
		}
	}
}

func TestDownloadSegmentsCleansPartialOutputOnFailure(t *testing.T) {
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	encrypted := encryptPaddedSegment(t, append([]byte("1234567890abcdef"), bytes.Repeat([]byte{16}, 16)...), key)
	for _, failure := range []string{"status", "short transfer", "invalid ciphertext", "invalid padding"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/one" {
					_, _ = w.Write(encrypted)
					return
				}
				switch failure {
				case "status":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "short transfer":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write(encrypted)
				case "invalid ciphertext":
					_, _ = w.Write([]byte("broken"))
				case "invalid padding":
					_, _ = w.Write(encryptPaddedSegment(t, make([]byte, 16), key))
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			app := &application{}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("downloadSegments() panicked: %v", recovered)
				}
				assertCatalogFiles(t, dir)
			}()
			got, err := app.downloadSegments(dir, []string{server.URL + "/one", server.URL + "/two"}, key, "")
			if got != "" || err == nil {
				t.Errorf("downloadSegments() = %q, %v; want empty path and error", got, err)
			}
		})
	}
}

func TestDownloadSegmentsRejectsEmptyAudio(t *testing.T) {
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	encrypted := encryptPaddedSegment(t, bytes.Repeat([]byte{16}, 16), key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(encrypted) }))
	defer server.Close()
	for _, urls := range [][]string{nil, {server.URL}} {
		dir := t.TempDir()
		app := &application{}
		if got, err := app.downloadSegments(dir, urls, key, ""); got != "" || err == nil {
			t.Errorf("downloadSegments() = %q, %v; want empty path and error", got, err)
		}
		assertCatalogFiles(t, dir)
	}
}

func installFFmpegStub(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell ffmpeg stub")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSaveTrackHLSOverwritePreservesOriginalOnRemuxFailure(t *testing.T) {
	installFFmpegStub(t, "exit 1\n")
	key := StreamKey{Value: make([]byte, aes.BlockSize), IV: make([]byte, aes.BlockSize)}
	encrypted := encryptPaddedSegment(t, append([]byte("1234567890abcdef"), bytes.Repeat([]byte{16}, 16)...), key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hls/playlist.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x00000000000000000000000000000000\n#EXTINF:10,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/hls/key":
			_, _ = w.Write(key.Value)
		case "/hls/segment.ts":
			_, _ = w.Write(encrypted)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "Existing.m4a")
	if err := os.WriteFile(path, []byte("original good audio"), 0600); err != nil {
		t.Fatal(err)
	}
	app := newTestApplication("overwrite")
	app.config.Quality = "medium-hls"
	app.config.TrackFileTemplate = "{name}"
	app.streamTrack = func(*beatport.Beatport, int64) (*beatport.TrackStream, error) {
		return &beatport.TrackStream{Url: server.URL + "/hls/playlist.m3u8"}, nil
	}
	if _, _, err := app.saveTrack(nil, &beatport.Track{ID: 1, Name: "Existing"}, dir, "medium-hls"); err == nil {
		t.Fatal("saveTrack() error = nil, want remux error")
	}
	assertFileContents(t, path, "original good audio")
	assertCatalogFiles(t, dir, "Existing.m4a")
}

func TestRemuxToM4APreservesOriginalOnFailure(t *testing.T) {
	for _, failure := range []string{"remux", "validation", "empty output", "missing output"} {
		t.Run(failure, func(t *testing.T) {
			installFFmpegStub(t, fmt.Sprintf(`if [ "$1" = "-v" ]; then
  [ "%s" != "validation" ]
  exit $?
fi
for arg do out="$arg"; done
if [ "%s" = "empty output" ]; then
  : > "$out"
elif [ "%s" != "missing output" ]; then
  printf replacement > "$out"
fi
[ "%s" != "remux" ]
`, failure, failure, failure, failure))
			dir := t.TempDir()
			input, output := filepath.Join(dir, "input.ts"), filepath.Join(dir, "output.m4a")
			if err := os.WriteFile(input, []byte("input"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(output, []byte("original good audio"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := remuxToM4A(input, output); err == nil {
				t.Error("remuxToM4A() error = nil, want failure")
			}
			assertFileContents(t, output, "original good audio")
			assertCatalogFiles(t, dir, "input.ts", "output.m4a")
		})
	}
}

func TestDownloadStagingSupportsLongDestinationNames(t *testing.T) {
	for _, remux := range []bool{false, true} {
		t.Run(fmt.Sprintf("remux=%v", remux), func(t *testing.T) {
			ext := ".flac"
			want := string(audioFixture(t, "tone.flac"))
			if remux {
				ext = ".m4a"
				want = "audio"
				installFFmpegStub(t, "if [ \"$1\" = \"-v\" ]; then exit 0; fi\nfor arg do out=\"$arg\"; done\nprintf audio > \"$out\"\n")
			}
			dir := t.TempDir()
			name := strings.Repeat("A", 255-len(ext)) + ext
			destination := filepath.Join(dir, name)
			if err := os.WriteFile(destination, []byte("original"), 0600); err != nil {
				t.Skipf("filesystem cannot store this filename: %v", err)
			}
			if remux {
				if err := remuxToM4A("unused-by-stub", destination); err != nil {
					t.Fatal(err)
				}
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(want)) }))
				defer server.Close()
				app := &application{}
				if err := app.downloadFile(server.URL, destination, ""); err != nil {
					t.Fatal(err)
				}
			}
			assertFileContents(t, destination, want)
			assertCatalogFiles(t, dir, name)
		})
	}
}

func TestRemuxToM4AWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("optional integration test requires ffmpeg")
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.ts"), filepath.Join(dir, "output.m4a")
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "0.25", "-c:a", "aac", "-f", "mpegts", input)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate synthetic audio: %v: %s", err, data)
	}
	if err := os.WriteFile(output, []byte("original"), 0666); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := remuxToM4A(input, output); err != nil {
		t.Fatal(err)
	}
	if err := validateM4A(output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("remuxed output permissions = %04o, want normal creation permissions %04o", info.Mode().Perm(), originalInfo.Mode().Perm())
	}
	assertCatalogFiles(t, dir, "input.ts", "output.m4a")
}
