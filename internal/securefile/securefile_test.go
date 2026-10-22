package securefile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadWriteAndAppendPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials")
	if err := Write(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); err != nil {
		t.Fatal(err)
	}
	f, err := OpenAppend(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("-append"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := Read(path)
	if err != nil || string(data) != "synthetic-append" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("mode = %04o", info.Mode().Perm())
		}
	}
}

func TestRejectsUnsafeLeavesWithoutChangingTarget(t *testing.T) {
	for _, kind := range []string{"symlink", "dangling", "hardlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "credentials")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "dangling":
				err = os.Symlink(filepath.Join(dir, "absent"), path)
			case "hardlink":
				err = os.Link(target, path)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Skipf("cannot create fixture: %v", err)
			}
			if err := Validate(path); err == nil {
				t.Error("Validate accepted unsafe leaf")
			}
			if _, err := Read(path); err == nil {
				t.Error("Read accepted unsafe leaf")
			}
			if err := Write(path, []byte("replacement")); err == nil {
				t.Error("Write accepted unsafe leaf")
			}
			if f, err := OpenAppend(path); err == nil {
				f.Close()
				t.Error("OpenAppend accepted unsafe leaf")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "unchanged" {
				t.Errorf("target = %q, %v", data, err)
			}
		})
	}
}

func TestMissingIsDistinguishable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "credentials")
	if err := Validate(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Validate = %v, want ErrNotExist", err)
	}
	if _, err := Read(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read = %v, want ErrNotExist", err)
	}
}

func TestReadReplacesBroadOwnedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode fixture")
	}
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("synthetic"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	data, err := Read(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	after, _ := os.Stat(path)
	if after.Mode().Perm() != 0600 || os.SameFile(before, after) {
		t.Errorf("unsafe migration: mode %04o, same inode %v", after.Mode().Perm(), os.SameFile(before, after))
	}
}

func TestCheckDirectoryDoesNotChangeMode(t *testing.T) {
	dir := t.TempDir()
	before, _ := os.Stat(dir)
	if err := CheckDirectory(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(dir)
	if before.Mode() != after.Mode() {
		t.Error("CheckDirectory changed directory mode")
	}
}

func TestWriteFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := Write(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	// A second hard link must make even a valid existing destination unwritable.
	if err := os.Link(path, filepath.Join(dir, "alias")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	if err := Write(path, []byte("replacement")); err == nil {
		t.Fatal("Write accepted linked original")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatalf("original changed to %q", data)
	}
}

func TestPinnedParentCannotBeRedirectedByAncestorRename(t *testing.T) {
	base := t.TempDir()
	original := filepath.Join(base, "original")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(original, "credentials")
	if err := Write(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	d, name, err := parent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	f, info, _, err := openExisting(d, name, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	moved := filepath.Join(base, "moved")
	if err := os.Rename(original, moved); err != nil {
		t.Skipf("directory rename unavailable: %v", err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("unrelated")); err != nil {
		t.Fatal(err)
	}
	if err := publish(d, name, []byte("replacement"), info); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "unrelated" {
		t.Fatalf("renamed ancestor redirected write: %q", data)
	}
	data, _ = os.ReadFile(filepath.Join(moved, name))
	if string(data) != "replacement" {
		t.Fatalf("pinned destination = %q", data)
	}
}

func TestPublishRejectsChangedDestinationAndCleansStaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := Write(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	d, name, err := parent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	f, info, _, err := openExisting(d, name, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := Write(path, []byte("concurrent replacement")); err != nil {
		t.Fatal(err)
	}
	if err := publish(d, name, []byte("stale data"), info); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("publish = %v, want unsafe error", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "concurrent replacement" {
		t.Fatalf("original lost: %q", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		t.Fatalf("staging leaked: %v", entries)
	}
}
