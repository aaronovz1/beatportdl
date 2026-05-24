package main

import (
	"os"
	"path"
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
