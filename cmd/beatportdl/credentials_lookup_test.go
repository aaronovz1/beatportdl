package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"unspok3n/beatportdl/internal/securefile"
)

func TestCredentialLookupPreservesTrustedPortableCWD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	file := filepath.Join(dir, cacheFilename)
	if err := securefile.Write(file, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	got, exists, err := FindCacheFile()
	if err != nil || !exists || got != file {
		t.Fatalf("FindCacheFile = %q, %v, %v", got, exists, err)
	}
}

func TestCredentialLookupRejectsSelectedUnsafeCandidate(t *testing.T) {
	for _, kind := range []string{"symlink", "dangling", "directory", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			path := filepath.Join(dir, cacheFilename)
			target := filepath.Join(dir, "target")
			if err := securefile.Write(target, []byte("unchanged")); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "dangling":
				err = os.Symlink(filepath.Join(dir, "missing"), path)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "hardlink":
				err = os.Link(target, path)
			}
			if err != nil {
				t.Skipf("fixture unavailable: %v", err)
			}
			got, exists, err := findFile(cacheFilename, []string{t.TempDir()})
			if !errors.Is(err, securefile.ErrUnsafe) || got != "" || exists {
				t.Errorf("findFile = %q, %v, %v, want unsafe error without fallback", got, exists, err)
			}
		})
	}
}

func TestCredentialLookupMissingUsesLastCandidate(t *testing.T) {
	t.Chdir(t.TempDir())
	last := t.TempDir()
	const name = "beatportdl-synthetic-no-real-file"
	got, exists, err := findFile(name, []string{last})
	if err != nil || exists || got != filepath.Join(last, name) {
		t.Fatalf("findFile = %q, %v, %v", got, exists, err)
	}
}

func TestCredentialLookupPreservesExecutableDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(filepath.Dir(executable), "beatportdl-synthetic-lookup-*")
	if err != nil {
		t.Skipf("test executable directory is read-only: %v", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	// Recreate using the same production API so Windows receives a protected DACL.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := securefile.Write(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	got, exists, err := findFile(filepath.Base(path), []string{t.TempDir()})
	if err != nil || !exists || got != path {
		t.Fatalf("findFile = %q, %v, %v", got, exists, err)
	}
}

func TestCredentialLookupDoesNotTreatPermissionErrorAsMissing(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged Unix permission enforcement")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0700)
	got, exists, err := findFile(filepath.Join("locked", "credentials"), []string{t.TempDir()})
	if !errors.Is(err, os.ErrPermission) || exists || got != "" {
		t.Fatalf("findFile = %q, %v, %v, want permission error", got, exists, err)
	}
}
