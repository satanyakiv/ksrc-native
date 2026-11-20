// Package executil runs external programs behind a small interface, so the
// rest of the tool can be tested without the real binaries.
package executil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Command describes one program run.
type Command struct {
	Path   string    // program name or path
	Args   []string  // arguments, without the program name
	Dir    string    // working directory; empty means the current one
	Stdout io.Writer // receives stdout; nil discards it
}

// Runner starts external programs.
type Runner interface {
	// LookPath finds a program on PATH, like exec.LookPath.
	LookPath(name string) (string, error)
	// Run executes the command and waits for it. A non-zero exit status is
	// reported as *ExitError; stderr is captured into that error.
	Run(ctx context.Context, cmd Command) error
}

// ExitError reports a program that ran and exited with a non-zero status.
type ExitError struct {
	Path   string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s exited with status %d", e.Path, e.Code)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// ExitCode returns the exit status carried by err, or -1 when err is not an
// *ExitError.
func ExitCode(err error) int {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// maxStderr bounds how much stderr is kept for error messages.
const maxStderr = 16 << 10

// OS is the Runner that starts real processes.
type OS struct{}

// LookPath implements Runner.
func (OS) LookPath(name string) (string, error) { return exec.LookPath(name) }

// Run implements Runner.
func (OS) Run(ctx context.Context, c Command) error {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Stdout = c.Stdout
	stderr := &tailBuffer{max: maxStderr}
	cmd.Stderr = stderr

	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{Path: c.Path, Code: exit.ExitCode(), Stderr: stderr.String()}
	}
	return fmt.Errorf("run %s: %w", c.Path, err)
}

// tailBuffer keeps only the last max bytes written to it.
type tailBuffer struct {
	max int
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return t.buf.String() }
