package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"

	"github.com/vbauerster/mpb/v8"
)

func TestBeatportSearchQuery(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		removed     bool
	}{
		{"Artist Track", "Artist Track", false},
		{"  Artist\tTrack  ", "Artist Track", false},
		{"@beatport Artist Track", "Artist Track", false},
		{"Artist @BEATPORT Track", "Artist Track", false},
		{"Artist @Example", "Artist @Example", false},
		{"Artist @beatsource", "", true},
		{"@beatport Artist @BEATSOURCE", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := beatportSearchQuery(tc.input)
			if got != tc.want || (err != nil) != tc.removed {
				t.Fatalf("query = %q, error = %v; want %q, removed=%v", got, err, tc.want, tc.removed)
			}
			if tc.removed && !errors.Is(err, beatport.ErrBeatsourceRemoved) {
				t.Fatalf("error = %v, want removal guidance", err)
			}
		})
	}
}

func TestSearchRejectsRemovedBeatsourceBeforeRequest(t *testing.T) {
	for _, query := range []string{"artist @beatsource", "@BEATSOURCE artist", "artist\t@beatsource"} {
		t.Run(query, func(t *testing.T) {
			var output bytes.Buffer
			app := &application{logWriter: &output}
			app.search(query)
			if !strings.Contains(output.String(), "Beatsource support has been removed") {
				t.Fatalf("expected migration guidance, got %q", output.String())
			}
		})
	}
}

func TestSearchRejectsInvalidSelectionsAndPreservesOrder(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "synthetic-token.json")
	hash := fnv.New64a()
	_, _ = hash.Write([]byte("synthetic:synthetic"))
	token, err := json.Marshal(map[string]interface{}{
		"access_token": "synthetic-token", "login_id": fmt.Sprintf("%x", hash.Sum(nil)),
		"issued_at": time.Now().Unix(), "expires_in": 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, token, 0600); err != nil {
		t.Fatal(err)
	}
	auth := beatport.NewAuth("synthetic", "synthetic", cachePath)
	if err := auth.LoadCache(); err != nil {
		t.Fatal(err)
	}
	bp := beatport.New("", auth)
	// Inject only the HTTP boundary; Beatport has no exported client setter.
	clientField := reflect.ValueOf(bp).Elem().FieldByName("client")
	client := reflect.NewAt(clientField.Type(), unsafe.Pointer(clientField.UnsafeAddr())).Elem().Interface().(*http.Client)
	client.Transport = searchSelectionTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/v4/catalog/search/" || req.URL.Query().Get("q") != "synthetic query" {
			return nil, fmt.Errorf("unexpected fixture request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{
			"tracks": [{"name":"Track One","url":"https://www.beatport.com/track/one/11"},{"name":"Track Two","url":"https://www.beatport.com/track/two/12"}],
			"releases": [{"name":"Release One","url":"https://www.beatport.com/release/one/21"},{"name":"Release Two","url":"https://www.beatport.com/release/two/22"}]
		}`))}, nil
	})

	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := inputWriter.WriteString("-1 4 0 2 nope 5 -999 3 1\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	stdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = stdin }()
	app := &application{bp: bp, config: &config.AppConfig{}, logWriter: io.Discard, urls: []string{"already-queued"}}
	app.search("synthetic query")
	want := []string{
		"already-queued",
		"https://www.beatport.com/release/two/22",
		"https://www.beatport.com/track/two/12",
		"https://www.beatport.com/release/one/21",
		"https://www.beatport.com/track/one/11",
	}
	if !reflect.DeepEqual(app.urls, want) {
		t.Fatalf("queued URLs = %q, want %q", app.urls, want)
	}
}

type searchSelectionTransport func(*http.Request) (*http.Response, error)

func (f searchSelectionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandleUrlRejectsRemovedBeatsourceBeforeRequest(t *testing.T) {
	var output bytes.Buffer
	app := &application{logWriter: &output}
	app.handleUrl("https://www.beatsource.com/release/example/42")
	if !strings.Contains(output.String(), "Beatsource support has been removed") {
		t.Fatalf("expected migration guidance, got %q", output.String())
	}
}

func TestMainPromptShowsMigrationGuidanceAfterProgressShutdown(t *testing.T) {
	for _, input := range []string{
		"Artist @beatsource",
		"https://www.beatsource.com/release/example/42",
		"HTTPS://WWW.BEATSOURCE.COM/release/example/42",
	} {
		t.Run(input, func(t *testing.T) { testMainPromptShowsMigrationGuidance(t, input) })
	}
}

func testMainPromptShowsMigrationGuidance(t *testing.T, query string) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := inputWriter.WriteString(query + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	output, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer outputWriter.Close()
	stdin, stdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, outputWriter
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()
	progress := mpb.New(mpb.WithOutput(io.Discard))
	progress.Shutdown()
	app := &application{pbp: progress, logWriter: progress}
	app.mainPrompt()
	for _, rawURL := range app.urls {
		app.handleUrl(rawURL)
	}
	_ = outputWriter.Close()
	message, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(message), "Beatsource support has been removed") {
		t.Fatalf("migration guidance lost after progress shutdown: %q", message)
	}
}
