package beatport

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCredentialCacheRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cache.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a := NewAuth("synthetic", "synthetic", link)
	a.tokenPair = &tokenPair{AccessToken: "synthetic", LoginID: a.loginId()}
	if err := a.WriteCache(); err == nil {
		t.Error("WriteCache accepted symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Errorf("target changed: %q, %v", data, err)
	}
}

func TestCredentialCachePrivateReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode assertion")
	}
	file := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(file)
	a := NewAuth("synthetic", "synthetic", file)
	a.tokenPair = &tokenPair{AccessToken: "synthetic", LoginID: a.loginId()}
	if err := a.WriteCache(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0600 {
		t.Errorf("mode = %04o, want 0600", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Error("WriteCache modified original inode")
	}
}

func TestCredentialCacheLoadAndWriteRejectLinkedValidCache(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original.json")
	a := NewAuth("synthetic", "synthetic", target)
	a.tokenPair = &tokenPair{AccessToken: "synthetic", LoginID: a.loginId()}
	if err := a.WriteCache(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(dir, kind+".json")
			var err error
			if kind == "symlink" {
				err = os.Symlink(target, path)
			} else {
				err = os.Link(target, path)
			}
			if err != nil {
				t.Skipf("fixture unavailable: %v", err)
			}
			defer os.Remove(path)
			loaded := NewAuth("synthetic", "synthetic", path)
			if err := loaded.LoadCache(); err == nil {
				t.Error("LoadCache accepted a linked valid cache")
			}
			loaded.tokenPair = &tokenPair{AccessToken: "replacement", LoginID: loaded.loginId()}
			if err := loaded.WriteCache(); err == nil {
				t.Error("WriteCache accepted a linked valid cache")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != string(original) {
				t.Errorf("target changed: %q, %v", data, err)
			}
		})
	}
}
