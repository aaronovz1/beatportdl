package config

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsUnsafeWhitespaceReplacement(t *testing.T) {
	for _, replacement := range []string{`/../../`, `\..\..\`, `C:\`, `:`, `*`, `?`, `"`, `<`, `>`, `|`, "\x00", "\n", "\t", "\x7f", "\u0080"} {
		t.Run(replacement, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yml")
			cfg := &AppConfig{Username: "test", Password: "test", DownloadsDirectory: t.TempDir(), WhitespaceCharacter: replacement}
			if err := cfg.Save(file); err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(file); err == nil || !strings.Contains(err.Error(), "whitespace_character") {
				t.Fatalf("unsafe replacement error = %v; want whitespace_character validation error", err)
			}
		})
	}
}

func TestParsePreservesSafeWhitespaceReplacements(t *testing.T) {
	for _, replacement := range []string{"", "_", "-", ".", " ", " - ", "\u00b7"} {
		t.Run(replacement, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yml")
			cfg := &AppConfig{Username: "test", Password: "test", DownloadsDirectory: t.TempDir(), WhitespaceCharacter: replacement}
			if err := cfg.Save(file); err != nil {
				t.Fatal(err)
			}
			got, err := Parse(file)
			if err != nil {
				t.Fatal(err)
			}
			if got.WhitespaceCharacter != replacement {
				t.Fatalf("replacement = %q, want %q", got.WhitespaceCharacter, replacement)
			}
		})
	}
}

func TestWhitespaceOverridesAreValidatedAfterApplication(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yml")
	cfg := &AppConfig{Username: "test", Password: "test", DownloadsDirectory: t.TempDir(), WhitespaceCharacter: "/../../"}
	if err := cfg.Save(file); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseCommandLine([]string{"--whitespace-character=_"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Parse(file, opts.Apply); err != nil || got.WhitespaceCharacter != "_" {
		t.Fatalf("safe override = %+v, %v", got, err)
	}
	for _, value := range []string{`/../../`, `\`, `:`, "\n"} {
		opts, err = ParseCommandLine([]string{"--whitespace-character=" + value}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Parse(file, opts.Apply); err == nil || !strings.Contains(err.Error(), "whitespace_character") {
			t.Errorf("unsafe CLI override %q accepted: %v", value, err)
		}
	}
}
