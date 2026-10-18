package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"golang.org/x/term"
)

type dependencies struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	random     io.Reader
	isTerminal func() bool
	readHidden func() ([]byte, error)
}

func main() {
	os.Exit(run(os.Args[1:], dependencies{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		random: rand.Reader,
		isTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd()))
		},
		readHidden: func() ([]byte, error) {
			return term.ReadPassword(int(os.Stdin.Fd()))
		},
	}))
}

func run(args []string, deps dependencies) int {
	if deps.stdout == nil || deps.stderr == nil || deps.random == nil || len(args) != 0 {
		writeFailure(deps.stderr)
		return 2
	}
	terminal := deps.isTerminal != nil && deps.isTerminal()
	var first, second []byte
	var err error
	if terminal {
		if deps.readHidden == nil {
			writeFailure(deps.stderr)
			return 1
		}
		_, _ = fmt.Fprint(deps.stderr, "Password: ")
		first, err = deps.readHidden()
		_, _ = fmt.Fprintln(deps.stderr)
		if err == nil {
			_, _ = fmt.Fprint(deps.stderr, "Repeat password: ")
			second, err = deps.readHidden()
			_, _ = fmt.Fprintln(deps.stderr)
		}
	} else {
		if deps.stdin == nil {
			writeFailure(deps.stderr)
			return 1
		}
		reader := bufio.NewReader(io.LimitReader(deps.stdin, 2*auth.MaxPasswordBytes+4))
		first, err = readPasswordLine(reader)
		if err == nil {
			second, err = readPasswordLine(reader)
		}
		if err == nil {
			_, trailingErr := reader.ReadByte()
			if trailingErr != io.EOF {
				err = errorsNewInput()
			}
		}
	}
	defer zero(first)
	defer zero(second)
	if err != nil || auth.ValidatePassword(first) != nil || !bytes.Equal(first, second) {
		writeFailure(deps.stderr)
		return 1
	}
	phc, err := auth.HashPassword(first, deps.random)
	if err != nil {
		writeFailure(deps.stderr)
		return 1
	}
	if _, err := fmt.Fprintln(deps.stdout, phc); err != nil {
		writeFailure(deps.stderr)
		return 1
	}
	return 0
}

func readPasswordLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) > auth.MaxPasswordBytes {
		return nil, errorsNewInput()
	}
	return line, nil
}

func writeFailure(stderr io.Writer) {
	if stderr != nil {
		_, _ = fmt.Fprintln(stderr, "adminhash: unable to generate password hash")
	}
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

type inputError struct{}

func (inputError) Error() string { return "invalid input" }
func errorsNewInput() error      { return inputError{} }
