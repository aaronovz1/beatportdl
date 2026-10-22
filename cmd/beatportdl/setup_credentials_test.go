package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"unspok3n/beatportdl/config"
)

func TestSetupFirstRunPreservesDefaults(t *testing.T) {
	if os.Getenv("BEATPORTDL_SETUP_FIXTURE") == "1" {
		cfg, _, err := Setup()
		if err != nil {
			t.Fatal(err)
		}
		want := config.DefaultConfig()
		want.Username = "synthetic-user"
		want.Password = "synthetic-password"
		want.DownloadsDirectory = filepath.Join(os.Getenv("BEATPORTDL_SETUP_FIXTURE_ROOT"), "downloads")
		want.Quality = "high"
		want.TagMappings = config.DefaultTagMappings
		if !reflect.DeepEqual(*cfg, want) {
			t.Fatalf("first-run configuration lost defaults: show_progress=%v fix_tags=%v track_template=%q release_template=%q artists_limit=%d padding=%d", cfg.ShowProgress, cfg.FixTags, cfg.TrackFileTemplate, cfg.ReleaseDirectoryTemplate, cfg.ArtistsLimit, cfg.TrackNumberPadding)
		}
		path, exists, err := FindConfigFile()
		if err != nil || !exists {
			t.Fatalf("saved config discovery: exists=%v err=%v", exists, err)
		}
		saved, err := config.Parse(path)
		if err != nil || !reflect.DeepEqual(saved, cfg) {
			t.Fatalf("saved config differs from first-run result: %v", err)
		}
		fmt.Println("SETUP_FIXTURE_COMPLETE")
		return
	}

	dir := t.TempDir()
	executable := filepath.Join(dir, filepath.Base(os.Args[0]))
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy test executable: %v, %v", copyErr, closeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSetupFirstRunPreservesDefaults$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BEATPORTDL_SETUP_FIXTURE=1", "BEATPORTDL_SETUP_FIXTURE_ROOT="+dir,
		"HOME="+dir, "USERPROFILE="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_STATE_HOME="+filepath.Join(dir, "state"))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr, output bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	for _, step := range []struct{ prompt, answer string }{
		{"Username: ", "synthetic-user"},
		{"Password: ", "synthetic-password"},
		{"Downloads directory: ", filepath.Join(dir, "downloads")},
		{"Quality: ", "2"},
	} {
		for !strings.HasSuffix(output.String(), step.prompt) {
			b, err := reader.ReadByte()
			if err != nil {
				t.Fatalf("waiting for %q: %v; output=%s", step.prompt, err, output.String())
			}
			output.WriteByte(b)
		}
		if _, err := fmt.Fprintln(stdin, step.answer); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdin.Close()
	_, readErr := io.Copy(&output, reader)
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil || !strings.Contains(output.String(), "SETUP_FIXTURE_COMPLETE") {
		t.Fatalf("first-run fixture: read=%v wait=%v\n%s\n%s", readErr, waitErr, output.String(), stderr.String())
	}
}
