package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

func TestRunHashesMatchingPipedPasswordsAndWritesOnlyPHCToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(nil, dependencies{
		stdin:  strings.NewReader("correct horse\ncorrect horse\n"),
		stdout: &stdout,
		stderr: &stderr,
		random: bytes.NewReader(bytes.Repeat([]byte{0x33}, 16)),
	})
	if exit != 0 {
		t.Fatalf("exit = %d, stderr=%q", exit, stderr.String())
	}
	phc := strings.TrimSuffix(stdout.String(), "\n")
	credentials, err := auth.ParseCredentials("admin", phc)
	if err != nil || credentials.Verify("admin", "correct horse") != nil {
		t.Fatalf("stdout is not the expected PHC: %q (%v)", stdout.String(), err)
	}
	if strings.Contains(stdout.String(), "correct horse") || strings.Contains(stderr.String(), "correct horse") {
		t.Fatal("password was echoed")
	}
}

func TestRunFailsSafelyForArgumentsEmptyMismatchAndInputFailure(t *testing.T) {
	secret := "do-not-echo"
	for name, testCase := range map[string]struct {
		args  []string
		input string
	}{
		"argument": {args: []string{secret}, input: secret + "\n" + secret + "\n"},
		"empty":    {input: "\n\n"},
		"mismatch": {input: secret + "\ndifferent\n"},
		"EOF":      {input: ""},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := run(testCase.args, dependencies{stdin: strings.NewReader(testCase.input), stdout: &stdout, stderr: &stderr, random: bytes.NewReader(make([]byte, 16))})
			if exit == 0 || stdout.Len() != 0 || strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
				t.Fatalf("unsafe failure: exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunReadsHiddenTerminalPasswordTwice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reads := 0
	exit := run(nil, dependencies{
		stdout:     &stdout,
		stderr:     &stderr,
		random:     bytes.NewReader(bytes.Repeat([]byte{0x44}, 16)),
		isTerminal: func() bool { return true },
		readHidden: func() ([]byte, error) {
			reads++
			if reads > 2 {
				return nil, errors.New("too many reads")
			}
			return []byte("hidden-secret"), nil
		},
	})
	if exit != 0 || reads != 2 || !strings.HasPrefix(stdout.String(), "$argon2id$") {
		t.Fatalf("terminal run: exit=%d reads=%d stdout=%q stderr=%q", exit, reads, stdout.String(), stderr.String())
	}
}

func TestRunUsesSharedPasswordLengthBoundary(t *testing.T) {
	for name, testCase := range map[string]struct {
		size     int
		wantExit int
		wantPHC  bool
	}{
		"maximum":   {size: auth.MaxPasswordBytes, wantExit: 0, wantPHC: true},
		"maximum+1": {size: auth.MaxPasswordBytes + 1, wantExit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			password := strings.Repeat("x", testCase.size)
			var stdout, stderr bytes.Buffer
			exit := run(nil, dependencies{
				stdin: strings.NewReader(password + "\n" + password + "\n"), stdout: &stdout, stderr: &stderr,
				random: bytes.NewReader(bytes.Repeat([]byte{0x71}, 16)),
			})
			if exit != testCase.wantExit || strings.HasPrefix(stdout.String(), "$argon2id$") != testCase.wantPHC {
				t.Fatalf("size %d: exit=%d stdout=%q stderr=%q", testCase.size, exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunRejectsAnyPipedInputAfterExactlyTwoLines(t *testing.T) {
	for name, input := range map[string]string{
		"third line":     "secret\nsecret\nthird\n",
		"trailing byte":  "secret\nsecret\nx",
		"trailing space": "secret\nsecret\n ",
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := run(nil, dependencies{
				stdin: strings.NewReader(input), stdout: &stdout, stderr: &stderr,
				random: bytes.NewReader(bytes.Repeat([]byte{0x81}, 16)),
			})
			if exit == 0 || stdout.Len() != 0 || stderr.String() != "adminhash: unable to generate password hash\n" {
				t.Fatalf("trailing input accepted: exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}
