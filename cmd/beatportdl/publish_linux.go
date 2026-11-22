package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func publishNewTrack(staged, target string) error {
	err := unix.Renameat2(unix.AT_FDCWD, staged, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		// Older filesystems may lack no-replace rename but support hard links.
		return os.Link(staged, target)
	}
	return err
}
