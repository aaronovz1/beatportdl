package config

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCommandLineLiteralSecretWarnings(t *testing.T) {
	var output bytes.Buffer
	cfg := &AppConfig{}
	opts, err := ParseCommandLine([]string{"--password", "fixture-pass", "--proxy=https://fixture-user:fixture-key@example.test"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	opts.Apply(cfg)
	if cfg.Password != "fixture-pass" || cfg.Proxy != "https://fixture-user:fixture-key@example.test" {
		t.Fatal("legacy flags no longer apply")
	}
	for _, flag := range []string{"--password-stdin", "--proxy-stdin"} {
		if !strings.Contains(output.String(), flag) {
			t.Errorf("warning did not suggest %s", flag)
		}
	}
	if strings.Contains(output.String(), "fixture-") {
		t.Fatal("warning echoed a secret")
	}
	output.Reset()
	if _, err := ParseCommandLine([]string{"--proxy=http://example.test:8080"}, &output); err != nil || output.Len() != 0 {
		t.Fatalf("ordinary proxy produced a warning or error: %q, %v", output.String(), err)
	}
}

func TestCommandLineSecretStdinOrderAndPrecedence(t *testing.T) {
	opts, err := ParseCommandLine([]string{"--proxy-stdin", "--password=old", "--password-stdin", "--proxy=http://example.test"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{"https://fixture-user:fixture-key@example.test", " fixture-pass "}
	err = opts.ReadSecrets(func() (string, error) {
		line := lines[0]
		lines = lines[1:]
		return line, nil
	})
	if err != nil || len(lines) != 0 || !opts.PasswordOverridden() {
		t.Fatalf("stdin options not resolved: remaining=%d, err=%v", len(lines), err)
	}
	cfg := &AppConfig{}
	opts.Apply(cfg)
	if cfg.Proxy != "http://example.test" || cfg.Password != " fixture-pass " {
		t.Fatal("stdin flags did not preserve option precedence or password whitespace")
	}

	opts, err = ParseCommandLine([]string{"--password-stdin"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	err = opts.ReadSecrets(func() (string, error) { return "unusable partial value", io.EOF })
	if !errors.Is(err, io.EOF) || strings.Contains(err.Error(), "partial value") {
		t.Fatalf("missing stdin secret not handled safely: %v", err)
	}
}
