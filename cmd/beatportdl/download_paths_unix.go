//go:build !windows

package main

import "os"

func isDownloadLink(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
