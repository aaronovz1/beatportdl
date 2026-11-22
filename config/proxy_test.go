package config

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"unspok3n/beatportdl/internal/securefile"
)

func TestProxyOverrideValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yml")
	if err := securefile.Write(file, []byte("username: user\npassword: pass\ndownloads_directory: /downloads\n")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"disabled", "", true},
		{"http", "http://127.0.0.1:8080", true},
		{"https authenticated", "https://user:fixture-secret@proxy.example.test/", true},
		{"socks", "socks5://proxy.example.test:1080", true},
		{"socks remote DNS", "socks5h://proxy.example.test:1080", true},
		{"IPv6", "http://[::1]:8080", true},
		{"missing scheme", "proxy.example.test:8080", false},
		{"missing host", "http://", false},
		{"invalid port", "http://proxy.example.test:65536", false},
		{"empty port", "http://proxy.example.test:", false},
		{"unbracketed IPv6", "http://::1", false},
		{"invalid IPv6", "https://[fixture]:8080", false},
		{"invalid URL", "https://user:fixture-secret@[%]", false},
		{"invalid scheme", "ftp://proxy.example.test", false},
		{"unexpected path", "http://proxy.example.test/path", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := ParseCommandLine([]string{"--proxy=" + tc.value}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(file, opts.Apply)
			if (err == nil) != tc.valid {
				t.Fatalf("parse error = %v, valid = %v", err, tc.valid)
			}
			if err != nil && (!strings.Contains(err.Error(), "proxy") || strings.Contains(err.Error(), "fixture-secret")) {
				t.Fatalf("unsafe or unclear validation error: %v", err)
			}
		})
	}
}
