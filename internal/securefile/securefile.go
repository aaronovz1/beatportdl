// Package securefile handles local files containing credentials or sensitive logs.
// Other processes running as the same user (and administrators) are trusted.
package securefile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsafe = errors.New("unsafe sensitive-file location")

type directory struct {
	root *os.Root
	file *os.File
}

func (d *directory) close() {
	_ = d.file.Close()
	_ = d.root.Close()
}

func unsafe(reason string) error { return fmt.Errorf("%w: %s", ErrUnsafe, reason) }

// CheckDirectory validates an existing directory and its ancestors without
// changing permissions. A caller using pathnames afterward must keep the same
// trusted-user threat model: this does not pin the caller's future operations.
func CheckDirectory(path string) error {
	d, err := openDirectory(path, false)
	if err != nil {
		return err
	}
	d.close()
	return nil
}

func newDirectory(root *os.Root) (*directory, error) {
	f, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	return &directory{root: root, file: f}, nil
}

// Open each component relative to the already validated directory handle.
// Trusted Unix aliases (notably /var -> private/var) are resolved explicitly;
// the final file is never allowed to be a symlink.
func openDirectory(path string, create bool) (_ *directory, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	base, parts, err := splitAbsolute(abs)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	d, err := newDirectory(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			d.close()
		}
	}()
	resolved := []string{}
	links := 0
	for len(parts) > 0 {
		if err = checkDirectoryHandle(d.file, false); err != nil {
			return nil, err
		}
		part := parts[0]
		parts = parts[1:]
		if err = checkName(part); err != nil {
			return nil, err
		}
		info, statErr := d.root.Lstat(part)
		if errors.Is(statErr, os.ErrNotExist) && create {
			if err = checkDirectoryHandle(d.file, true); err != nil {
				return nil, err
			}
			if err = makeDirectory(d, part); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, err
			}
			info, statErr = d.root.Lstat(part)
		}
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err = checkAlias(info); err != nil {
				return nil, err
			}
			links++
			if links > 40 {
				return nil, unsafe("too many directory aliases")
			}
			var target string
			target, err = d.root.Readlink(part)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(append([]string{base}, append(resolved, target)...)...)
			}
			if len(parts) > 0 {
				target = filepath.Join(append([]string{target}, parts...)...)
			}
			base, parts, err = splitAbsolute(filepath.Clean(target))
			if err != nil {
				return nil, err
			}
			r, err = os.OpenRoot(base)
			if err != nil {
				return nil, err
			}
			next, openErr := newDirectory(r)
			if openErr != nil {
				return nil, openErr
			}
			d.close()
			d = next
			resolved = nil
			continue
		}
		if !info.IsDir() {
			return nil, unsafe("ancestor is not a directory")
		}
		r, err = d.root.OpenRoot(part)
		if err != nil {
			return nil, err
		}
		next, openErr := newDirectory(r)
		if openErr != nil {
			return nil, openErr
		}
		opened, statErr := next.file.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			next.close()
			return nil, unsafe("directory changed while opening")
		}
		d.close()
		d = next
		resolved = append(resolved, part)
	}
	if err = checkDirectoryHandle(d.file, true); err != nil {
		return nil, err
	}
	return d, nil
}

func parent(path string, create bool) (*directory, string, error) {
	if path == "" {
		return nil, "", unsafe("empty filename")
	}
	name := filepath.Base(path)
	if name == "." || name == ".." || strings.ContainsAny(name, "\x00") {
		return nil, "", unsafe("invalid filename")
	}
	if err := checkName(name); err != nil {
		return nil, "", err
	}
	d, err := openDirectory(filepath.Dir(path), create)
	return d, name, err
}

func openExisting(d *directory, name string, appendMode bool) (*os.File, os.FileInfo, bool, error) {
	info, err := d.root.Lstat(name)
	if err != nil {
		return nil, nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, false, unsafe("not a regular file (symlinks are not accepted)")
	}
	f, err := openLeaf(d, name, appendMode)
	if err != nil {
		return nil, nil, false, err
	}
	opened, err := f.Stat()
	if err == nil && !os.SameFile(info, opened) {
		err = unsafe("file changed while opening")
	}
	private := false
	if err == nil {
		private, err = checkFileHandle(f, opened)
	}
	if err != nil {
		f.Close()
		return nil, nil, false, err
	}
	return f, opened, private, nil
}

// Validate checks discovery candidates without modifying them. Absence remains
// distinguishable with errors.Is(err, os.ErrNotExist); all other errors are fatal.
func Validate(path string) error {
	d, name, err := parent(path, false)
	if err != nil {
		return err
	}
	defer d.close()
	f, _, _, err := openExisting(d, name, false)
	if err != nil {
		return err
	}
	return f.Close()
}

// Read tightens broadly readable owned files by replacing their inode, never by
// chmodding a possibly linked inode. Unsafe writable files are rejected instead.
func Read(path string) ([]byte, error) {
	d, name, err := parent(path, false)
	if err != nil {
		return nil, err
	}
	defer d.close()
	f, info, private, err := openExisting(d, name, false)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if !private {
		if err := publish(d, name, data, info); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// Write publishes fully encoded data using an exclusive private sibling file.
// A failed write, close, or rename leaves the previous destination intact.
func Write(path string, data []byte) error {
	d, name, err := parent(path, true)
	if err != nil {
		return err
	}
	defer d.close()
	var expected os.FileInfo
	f, info, _, err := openExisting(d, name, false)
	if err == nil {
		expected = info
		if err := f.Close(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return publish(d, name, data, expected)
}

func publish(d *directory, name string, data []byte, expected os.FileInfo) error {
	tempName := ".beatportdl-private-" + rand.Text()
	f, err := createLeaf(d, tempName)
	if err != nil {
		return err
	}
	defer d.root.Remove(tempName)
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Revalidate the leaf immediately before replacement. The directory handle
	// pins the destination, so ancestor renames cannot redirect publication.
	current, statErr := d.root.Lstat(name)
	if expected == nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			if statErr != nil {
				return statErr
			}
			return unsafe("destination appeared during write")
		}
	} else {
		if statErr != nil {
			return statErr
		}
		if !os.SameFile(expected, current) || expected.Size() != current.Size() || !expected.ModTime().Equal(current.ModTime()) {
			return unsafe("destination changed during write")
		}
		old, _, _, err := openExisting(d, name, false)
		if err != nil {
			return err
		}
		if err := old.Close(); err != nil {
			return err
		}
	}
	return d.root.Rename(tempName, name)
}

// OpenAppend opens a private log without following links. Legacy broadly
// readable files are copied to a private replacement before append is allowed.
func OpenAppend(path string) (*os.File, error) {
	d, name, err := parent(path, true)
	if err != nil {
		return nil, err
	}
	defer d.close()
	f, info, private, err := openExisting(d, name, true)
	if errors.Is(err, os.ErrNotExist) {
		if err := publish(d, name, nil, nil); err != nil {
			return nil, err
		}
		f, _, _, err = openExisting(d, name, true)
		return f, err
	}
	if err != nil {
		return nil, err
	}
	if private {
		return f, nil
	}
	f.Close()
	f, _, _, err = openExisting(d, name, false)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := publish(d, name, data, info); err != nil {
		return nil, err
	}
	f, _, _, err = openExisting(d, name, true)
	return f, err
}
