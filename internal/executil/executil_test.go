package executil

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOSRunCapturesStdout(t *testing.T) {
	var out bytes.Buffer
	err := OS{}.Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "printf hello"}, Stdout: &out})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.String() != "hello" {
		t.Errorf("stdout = %q, want %q", out.String(), "hello")
	}
}

func TestOSRunReportsExitStatusAndStderr(t *testing.T) {
	err := OS{}.Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "echo broken >&2; exit 3"}})

	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Run() error = %v, want *ExitError", err)
	}
	if exit.Code != 3 {
		t.Errorf("Code = %d, want 3", exit.Code)
	}
	if !strings.Contains(exit.Error(), "broken") {
		t.Errorf("Error() = %q, want it to contain stderr", exit.Error())
	}
	if got := ExitCode(err); got != 3 {
		t.Errorf("ExitCode() = %d, want 3", got)
	}
}

func TestOSRunMissingProgram(t *testing.T) {
	err := OS{}.Run(context.Background(), Command{Path: "ksrc-native-no-such-program"})
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if got := ExitCode(err); got != -1 {
		t.Errorf("ExitCode() = %d, want -1 for a program that did not start", got)
	}
}

func TestOSRunUsesDir(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := (OS{}).Run(context.Background(), Command{Path: "pwd", Args: []string{"-P"}, Dir: dir, Stdout: &out}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(out.String()), strings.TrimPrefix(dir, "/private")) {
		t.Errorf("pwd = %q, want it to end with %q", out.String(), dir)
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	b := &tailBuffer{max: 4}
	b.Write([]byte("abcdef"))
	b.Write([]byte("gh"))
	if got := b.String(); got != "efgh" {
		t.Errorf("tail = %q, want %q", got, "efgh")
	}
}

func TestOSLookPath(t *testing.T) {
	if _, err := (OS{}).LookPath("sh"); err != nil {
		t.Errorf("LookPath(sh) error = %v", err)
	}
}
