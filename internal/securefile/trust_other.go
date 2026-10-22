//go:build !darwin && !linux && !windows

package securefile

import "os"

func splitAbsolute(string) (string, []string, error) { return "", nil, unsafe("unsupported platform") }
func checkName(string) error                         { return unsafe("unsupported platform") }
func checkAlias(os.FileInfo) error                   { return unsafe("unsupported platform") }
func checkDirectoryHandle(*os.File, bool) error      { return unsafe("unsupported platform") }
func checkFileHandle(*os.File, os.FileInfo) (bool, error) {
	return false, unsafe("unsupported platform")
}
func openLeaf(*directory, string, bool) (*os.File, error) { return nil, unsafe("unsupported platform") }
func createLeaf(*directory, string) (*os.File, error)     { return nil, unsafe("unsupported platform") }
func makeDirectory(*directory, string) error              { return unsafe("unsupported platform") }
