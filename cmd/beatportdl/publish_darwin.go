package main

import "golang.org/x/sys/unix"

func publishNewTrack(staged, target string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, staged, unix.AT_FDCWD, target, unix.RENAME_EXCL)
}
