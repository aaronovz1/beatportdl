package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
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
