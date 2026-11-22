package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"unspok3n/beatportdl/internal/securefile"
)

// TagLib and FFmpeg use pathnames. Require trusted directories throughout the
// write path, and do not follow aliases in app-generated subdirectories.
func prepareDownloadDirectory(root, directory string, create bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || !filepath.IsLocal(relative) {
		return errors.New("download directory must remain within the configured root")
	}
	if create {
		if err := createDownloadRoot(root); err != nil {
			return err
		}
	}
	if err := securefile.CheckDirectory(root); err != nil {
		return fmt.Errorf("unsafe download root: %w", err)
	}
	current := root
	if relative != "." {
		for _, name := range strings.Split(relative, string(os.PathSeparator)) {
			current = filepath.Join(current, name)
			info, err := os.Lstat(current)
			if create && errors.Is(err, os.ErrNotExist) {
				if err := os.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
					return err
				}
				info, err = os.Lstat(current)
			}
			if err != nil {
				return err
			}
			if !info.IsDir() || isDownloadLink(info) {
				return fmt.Errorf("not a regular download directory: %q", current)
			}
			if err := securefile.CheckDirectory(current); err != nil {
				return fmt.Errorf("unsafe download directory: %w", err)
			}
		}
	}
	return nil
}

func createDownloadRoot(path string) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return fmt.Errorf("download volume is unavailable: %q", path)
	}
	if err := createDownloadRoot(parent); err != nil {
		return err
	}
	if err := securefile.CheckDirectory(parent); err != nil {
		return fmt.Errorf("unsafe parent of download root: %w", err)
	}
	if err := os.Mkdir(path, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func (app *application) validateDownloadDirectory(directory string) error {
	if app.config != nil && app.config.DownloadsDirectory != "" {
		return prepareDownloadDirectory(app.config.DownloadsDirectory, directory, false)
	}
	return prepareDownloadDirectory(directory, directory, false)
}

// A missing file returns nil, while a link or other special entry is rejected.
func inspectDownloadFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || isDownloadLink(info) {
		return nil, fmt.Errorf("not a regular download file: %q", path)
	}
	return info, nil
}
