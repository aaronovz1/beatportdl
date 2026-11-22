//go:build darwin || linux

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func (in *cliInput) Read(buf []byte) (int, error) {
	fds := []unix.PollFd{{Fd: int32(in.file.Fd()), Events: unix.POLLIN}}
	for in.ctx.Err() == nil {
		// Inherited stdin is not necessarily usable with os.File.SetReadDeadline.
		ready, err := unix.Poll(fds, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready > 0 && in.ctx.Err() == nil {
			return in.file.Read(buf)
		}
	}
	return 0, in.ctx.Err()
}

func disableInputEcho(file *os.File) (func() error, error) {
	fd := int(file.Fd())
	state, err := unix.IoctlGetTermios(fd, getInputTermios)
	if err != nil {
		return nil, err
	}
	hidden := *state
	hidden.Lflag &^= unix.ECHO | unix.ECHONL
	if err := unix.IoctlSetTermios(fd, setInputTermios, &hidden); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(fd, setInputTermios, state) }, nil
}
