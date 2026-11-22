package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type cliInput struct {
	ctx    context.Context
	file   *os.File
	reader *bufio.Reader
	tty    bool
}

func newCLIInput(ctx context.Context, file *os.File) *cliInput {
	input := &cliInput{ctx: ctx, file: file, tty: term.IsTerminal(int(file.Fd()))}
	input.reader = bufio.NewReader(input)
	return input
}

func (in *cliInput) line() (string, error) {
	if err := in.ctx.Err(); err != nil {
		return "", err
	}
	line, err := in.reader.ReadString('\n')
	if canceled := in.ctx.Err(); canceled != nil {
		return "", canceled
	}
	if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func (in *cliInput) secretLine() (string, error) {
	if in.tty {
		return "", errors.New("stdin must be redirected")
	}
	return in.line()
}

func (in *cliInput) password() (value string, err error) {
	if !in.tty {
		return in.line()
	}
	restore, err := disableInputEcho(in.file)
	if err != nil {
		return "", err
	}
	defer func() {
		if restoreErr := restore(); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal echo: %w", restoreErr))
		}
		fmt.Println()
	}()
	return in.line()
}
