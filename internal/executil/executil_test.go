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

// java -version prints to stderr, so a caller can ask for it.
func TestOSRunStderrGoesToTheWriterOfTheCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := OS{}.Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "echo out; echo version 17 >&2"}, Stdout: &stdout, Stderr: &stderr})
	if err != nil || stdout.String() != "out\n" || stderr.String() != "version 17\n" {
		t.Errorf("Run() = %v, stdout %q, stderr %q; want nil, %q, %q", err, stdout.String(), stderr.String(), "out\n", "version 17\n")
	}

	// A failure still carries stderr in the error.
	stderr.Reset()
	err = OS{}.Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "echo broken >&2; exit 3"}, Stderr: &stderr})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Stderr != "broken\n" || stderr.String() != "broken\n" {
		t.Errorf("Run() error = %v with writer %q; want an exit error and the writer both holding %q", err, stderr.String(), "broken\n")
	}
}

func TestSummary(t *testing.T) {
	long := strings.Repeat("Exception in thread main\n\tat org.jetbrains.Kotlin.run(Kotlin.kt:1)\n", 40)
	tests := []struct {
		name string
		err  error
		max  int
		want string
	}{
		{"stderr of the program, folded into one line", &ExitError{Path: "klib", Code: 1, Stderr: "error: library not found:\n  /klib/UIKit\n"}, 300, "error: library not found: /klib/UIKit"},
		{"no stderr: the error text", &ExitError{Path: "klib", Code: 2}, 300, "klib exited with status 2"},
		{"not an exit error", errors.New("fork/exec klib:\n permission denied"), 300, "fork/exec klib: permission denied"},
		{"cut at the limit", &ExitError{Path: "klib", Code: 1, Stderr: long}, 40, "Exception in thread main at org.jetbrain..."},
		{"cut between characters, not inside one", &ExitError{Path: "klib", Code: 1, Stderr: "помилка: бібліотеку не знайдено"}, 10, "помил..."},
		{"no error", nil, 300, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summary(tt.err, tt.max); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}
