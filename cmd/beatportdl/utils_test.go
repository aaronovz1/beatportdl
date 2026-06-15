package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestFindConfigFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG config lookup applies only on Linux")
	}

	xdgConfigHome := "/tmp/foo/bar"

	t.Run("Use default XDG_CONFIG_HOME without env being set", func(t *testing.T) {
		unsetEnv(t, "XDG_CONFIG_HOME")

		configFilePath, _, gotErr := FindConfigFile()
		if gotErr != nil {
			t.Errorf("FindConfigFile() failed: %v", gotErr)
			return
		}

		expectedPath := path.Join(os.Getenv("HOME"), ".config", "beatportdl", configFilename)

		if expectedPath != configFilePath {
			t.Errorf("Paths do not match %s != %s", expectedPath, configFilePath)
		}
	})

	t.Run("Use XDG_CONFIG_HOME with env being set", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		configFilePath, _, gotErr := FindConfigFile()
		if gotErr != nil {
			t.Errorf("FindConfigFile() failed: %v", gotErr)
			return
		}

		expectedPath := path.Join(xdgConfigHome, "beatportdl", configFilename)

		if expectedPath != configFilePath {
			t.Errorf("Paths do not match %s != %s", expectedPath, configFilePath)
		}
	})
}

func TestFindCacheFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG state lookup applies only on Linux")
	}

	xdgStateHome := "/tmp/foo/bar"

	t.Run("Use default XDG_STATE_HOME without env being set", func(t *testing.T) {
		unsetEnv(t, "XDG_STATE_HOME")

		cacheFilePath, _, gotErr := FindCacheFile()
		if gotErr != nil {
			t.Errorf("FindCacheFile() failed: %v", gotErr)
			return
		}

		expectedPath := path.Join(os.Getenv("HOME"), ".local/state", "beatportdl", cacheFilename)

		if expectedPath != cacheFilePath {
			t.Errorf("Paths do not match %s != %s", expectedPath, cacheFilePath)
		}
	})

	t.Run("Use XDG_STATE_HOME with env being set", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", xdgStateHome)

		cacheFilePath, _, gotErr := FindCacheFile()
		if gotErr != nil {
			t.Errorf("FindCacheFile() failed: %v", gotErr)
			return
		}

		expectedPath := path.Join(xdgStateHome, "beatportdl", cacheFilename)

		if expectedPath != cacheFilePath {
			t.Errorf("Paths do not match, %s != %s", expectedPath, cacheFilePath)
		}
	})
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	previous, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s): %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestClampProgressPrefixLeavesShortStringsUntouched(t *testing.T) {
	got := clampProgressPrefix("Short title [FLAC]", 72)
	want := "Short title [FLAC]"

	if got != want {
		t.Fatalf("clampProgressPrefix() = %q, want %q", got, want)
	}
}

func TestClampProgressPrefixTruncatesLongStrings(t *testing.T) {
	input := strings.Repeat("Very Long Track Name ", 8) + "[FLAC]"

	got := clampProgressPrefix(input, 32)

	if got == input {
		t.Fatalf("expected truncated prefix, got original %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("clampProgressPrefix() = %q, want ellipsis suffix", got)
	}
	if runewidth.StringWidth(got) > 32 {
		t.Fatalf("clampProgressPrefix() width = %d, want <= 32", runewidth.StringWidth(got))
	}
}

func TestClampProgressPrefixHandlesVerySmallWidths(t *testing.T) {
	got := clampProgressPrefix("Anything", 2)
	want := ".."

	if got != want {
		t.Fatalf("clampProgressPrefix() = %q, want %q", got, want)
	}
}

func TestDownloadFileRemovesDestinationOnShortTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "track.m4a")
	app := &application{}

	if err := app.downloadFile(server.URL, destination, ""); err == nil {
		t.Fatal("downloadFile() error = nil, want short-transfer error")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("expected destination to be absent after failed download, stat err = %v", err)
	}
}

func TestRemuxToM4ARejectsInvalidOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell ffmpeg stub")
	}

	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	stub := `#!/bin/sh
if [ "$1" = "-v" ]; then
  printf 'partial file\n' >&2
  exit 0
fi
out=""
for arg do
  out="$arg"
done
printf invalid > "$out"
exit 0
`
	if err := os.WriteFile(ffmpeg, []byte(stub), 0755); err != nil {
		t.Fatalf("WriteFile(%s): %v", ffmpeg, err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s%c%s", dir, os.PathListSeparator, os.Getenv("PATH")))

	input := filepath.Join(dir, "input.ts")
	output := filepath.Join(dir, "output.m4a")
	if err := os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", input, err)
	}

	if err := remuxToM4A(input, output); err == nil {
		t.Fatal("remuxToM4A() error = nil, want validation error")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("expected invalid output to be removed, stat err = %v", err)
	}
}
