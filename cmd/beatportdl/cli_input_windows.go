package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func (in *cliInput) Read(buf []byte) (int, error) {
	// A console read may outlive cancellation until the CLI exits. It must not
	// keep writing to the buffered reader's storage or own the console mode.
	type result struct {
		n   int
		err error
	}
	scratch := make([]byte, len(buf))
	done := make(chan result, 1)
	go func() {
		n, err := in.file.Read(scratch)
		done <- result{n, err}
	}()
	select {
	case <-in.ctx.Done():
		return 0, in.ctx.Err()
	case r := <-done:
		return copy(buf, scratch[:r.n]), r.err
	}
}

func disableInputEcho(file *os.File) (func() error, error) {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, mode&^windows.ENABLE_ECHO_INPUT); err != nil {
		return nil, err
	}
	return func() error { return windows.SetConsoleMode(handle, mode) }, nil
}
