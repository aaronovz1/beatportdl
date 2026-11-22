package main

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func publishNewTrack(staged, target string) error {
	from, err := absoluteWindowsPath(staged)
	if err != nil {
		return err
	}
	to, err := absoluteWindowsPath(target)
	if err != nil {
		return err
	}
	return windows.MoveFile(from, to)
}

func absoluteWindowsPath(path string) (*uint16, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(absolute, `\\?\`) {
		absolute = `\\?\` + absolute
	}
	return windows.UTF16PtrFromString(absolute)
}
