//go:build darwin || linux

package securefile

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func splitAbsolute(path string) (string, []string, error) {
	parts := strings.FieldsFunc(filepath.Clean(path), func(r rune) bool { return r == '/' })
	return "/", parts, nil
}

func checkName(string) error { return nil }

func checkAlias(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) {
		return unsafe("directory alias has an untrusted owner")
	}
	return nil
}

func checkDirectoryHandle(f *os.File, final bool) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return unsafe("not a directory")
	}
	if stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0 {
		return unsafe("directory has an untrusted owner")
	}
	if info.Mode().Perm()&0022 != 0 {
		// Sticky ancestors are safe only because every selected child is checked
		// for trusted ownership. They are never suitable final write locations.
		if final || info.Mode()&os.ModeSticky == 0 {
			return unsafe("directory is group/world writable")
		}
	}
	return checkDirectoryACL(f, final)
}

func checkFileHandle(f *os.File, info os.FileInfo) (bool, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return false, unsafe("not a regular file")
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return false, unsafe("file is not owned by the current user")
	}
	if stat.Nlink != 1 {
		return false, unsafe("file has multiple hard links")
	}
	if info.Mode().Perm()&0022 != 0 {
		return false, unsafe("file is group/world writable")
	}
	aclPrivate, err := checkFileACL(f)
	return info.Mode().Perm()&0177 == 0 && aclPrivate, err
}

func openLeaf(d *directory, name string, appendMode bool) (*os.File, error) {
	flags := os.O_RDONLY
	if appendMode {
		flags = os.O_WRONLY | os.O_APPEND
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func createLeaf(d *directory, name string) (*os.File, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	// This is our new exclusive inode, never an existing linked destination.
	if err := f.Chmod(0600); err != nil {
		f.Close()
		d.root.Remove(name)
		return nil, err
	}
	info, err := f.Stat()
	if err == nil {
		var private bool
		private, err = checkFileHandle(f, info)
		if err == nil && !private {
			err = unsafe("new file inherited a permissive ACL")
		}
	}
	if err != nil {
		f.Close()
		d.root.Remove(name)
		return nil, err
	}
	return f, nil
}

func makeDirectory(d *directory, name string) error { return d.root.Mkdir(name, 0700) }
