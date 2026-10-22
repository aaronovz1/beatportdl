//go:build darwin || linux

package securefile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRejectsWritableParentAndAncestor(t *testing.T) {
	for _, mode := range []os.FileMode{0770, 0777, 0777 | os.ModeSticky} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			if err := CheckDirectory(dir); err == nil {
				t.Error("accepted writable final parent")
			}
			if err := Write(filepath.Join(dir, "credentials"), []byte("synthetic")); err == nil {
				t.Error("Write accepted writable parent")
			}
			child := filepath.Join(dir, "owned")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			if mode&os.ModeSticky == 0 {
				if err := CheckDirectory(child); err == nil {
					t.Error("accepted writable ancestor")
				}
			} else {
				if err := CheckDirectory(child); err != nil {
					t.Errorf("rejected trusted child of sticky ancestor: %v", err)
				}
			}
		})
	}
}

func TestTrustedAncestorAlias(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(alias, "credentials"), []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
}

func TestFIFORejectedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); err == nil {
		t.Error("Validate accepted FIFO")
	}
	if _, err := Read(path); err == nil {
		t.Error("Read accepted FIFO")
	}
	if err := Write(path, nil); err == nil {
		t.Error("Write accepted FIFO")
	}
}

func TestRejectsWritableSensitiveFileWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); err == nil {
		t.Error("Validate accepted writable file")
	}
	if _, err := Read(path); err == nil {
		t.Error("Read accepted writable file")
	}
	if err := Write(path, []byte("replacement")); err == nil {
		t.Error("Write accepted writable file")
	}
	if f, err := OpenAppend(path); err == nil {
		f.Close()
		t.Error("OpenAppend accepted writable file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "unchanged" {
		t.Fatalf("writable file changed: %q", data)
	}
}

func TestRejectsOtherOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing fixture ownership requires root (covered by Linux container CI)")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 1, -1); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); err == nil {
		t.Error("Validate accepted another owner's file")
	}
	if _, err := Read(path); err == nil {
		t.Error("Read accepted another owner's file")
	}
	if err := Write(path, []byte("replacement")); err == nil {
		t.Error("Write accepted another owner's file")
	}
	child := filepath.Join(dir, "foreign")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(child, 1, -1); err != nil {
		t.Fatal(err)
	}
	if err := CheckDirectory(child); err == nil {
		t.Error("accepted another owner's directory")
	}
}
