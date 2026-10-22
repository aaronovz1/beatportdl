package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unspok3n/beatportdl/internal/beatport"
)

func TestReleaseLabelSortingContainsMetadata(t *testing.T) {
	for _, name := range []string{"../../outside", `..\..\outside`, "/absolute", ".", "..", "CON", "Example / Label", "Normal Label"} {
		for _, context := range []bool{false, true} {
			for _, label := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/context=%v/label=%v", name, context, label), func(t *testing.T) {
					base := filepath.Join(t.TempDir(), "root", "downloads")
					if err := os.MkdirAll(base, 0700); err != nil {
						t.Fatal(err)
					}
					app := newTestApplication("skip")
					app.config.SortByContext, app.config.SortByLabel = context, label
					app.config.ReleaseDirectoryTemplate = "Archive/{name}"
					release := &beatport.Release{Name: "Release", Label: beatport.Label{Name: name}}
					got, err := app.setupDownloadsDirectory(base, release)
					if err != nil {
						t.Fatal(err)
					}
					want := base
					if context {
						if label {
							want = filepath.Join(want, beatport.SanitizePath(name, ""))
						}
						want = filepath.Join(want, "Archive", "Release")
					}
					if got != want {
						t.Fatalf("directory = %q, want %q", got, want)
					}
					rel, err := filepath.Rel(base, got)
					if err != nil || !filepath.IsLocal(rel) {
						t.Fatalf("directory escaped root: %q (%v)", got, err)
					}
				})
			}
		}
	}
}

func TestRemuxUsesPrivateStagingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ffmpeg test double; Windows directory ACLs have separate tests")
	}
	marker := filepath.Join(t.TempDir(), "observed-path")
	t.Setenv("STAGING_MARKER", marker)
	installFFmpegStub(t, `if [ "$1" = "-v" ]; then exit 0; fi
for arg do out="$arg"; done
printf '%s' "$out" > "$STAGING_MARKER"
printf audio > "$out"
`)
	dir := t.TempDir()
	destination := filepath.Join(dir, "track.m4a")
	if err := remuxToM4A("unused-by-stub", destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Dir(string(data))
	if stage == dir || filepath.Dir(stage) != dir || !strings.HasPrefix(filepath.Base(stage), ".beatportdl-remux-") {
		t.Fatalf("FFmpeg output %q was not inside a private sibling staging directory", data)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("staging directory remains: %v", err)
	}
	assertFileContents(t, destination, "audio")
	assertCatalogFiles(t, dir, "track.m4a")
}

func TestRemuxRejectsSharedWritableDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory trust is enforced with ACLs")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	output := filepath.Join(dir, "output.m4a")
	if err := remuxToM4A("unused", output); err == nil || !strings.Contains(err.Error(), "unsafe remux destination") {
		t.Fatalf("shared writable remux directory accepted: %v", err)
	}
	assertCatalogFiles(t, dir)
}
