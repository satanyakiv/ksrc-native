//go:build unix

package executil

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// tree is a program shaped like <konanHome>/bin/klib: a shell script that
// starts a second script without exec, which starts the program that runs
// for a long time. Killing the first process alone leaves the other two
// alive, and they keep the stdout pipe open.
type tree struct {
	outer      string
	innerPid   string // file with the pid of the second script
	sleeperPid string // file with the pid of the long-running program
}

func newTree(t *testing.T) tree {
	t.Helper()
	dir := t.TempDir()
	tr := tree{
		outer:      filepath.Join(dir, "outer.sh"),
		innerPid:   filepath.Join(dir, "inner.pid"),
		sleeperPid: filepath.Join(dir, "sleeper.pid"),
	}
	inner := filepath.Join(dir, "inner.sh")
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(tr.outer, "#!/bin/sh\necho started\n'"+inner+"'\n")
	write(inner, "#!/bin/sh\nsleep 30 &\necho $! > '"+tr.sleeperPid+"'\necho $$ > '"+tr.innerPid+"'\nwait\n")
	t.Cleanup(func() {
		for _, file := range []string{tr.innerPid, tr.sleeperPid} {
			if pid, ok := readPid(file); ok {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return tr
}

func readPid(file string) (int, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, err == nil && pid > 1
}

// waitForPids waits until both scripts have written their pid files.
func (tr tree) waitForPids(t *testing.T) (inner, sleeper int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		i, ok1 := readPid(tr.innerPid)
		s, ok2 := readPid(tr.sleeperPid)
		if ok1 && ok2 {
			return i, s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the test program did not start within 10 s")
	return 0, 0
}

// gone reports whether the process has ended, waiting up to 3 s for it.
func gone(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestOSRunCancelStopsTheWholeProcessTree(t *testing.T) {
	tr := newTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- OS{}.Run(ctx, Command{Path: tr.outer, Stdout: &out}) }()
	inner, sleeper := tr.waitForPids(t)

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want one that wraps context.Canceled", err)
		}
		if ExitCode(err) != -1 {
			t.Errorf("Run() error = %v is an exit status; a cancelled run is not a failed program", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within 5 s of the cancel: the grandchildren hold the stdout pipe")
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("Run() returned %s after the cancel, want well under the 2 s wait delay", took)
	}
	if !gone(inner) || !gone(sleeper) {
		t.Errorf("after the cancel: second script gone = %v, long-running program gone = %v; want both gone", gone(inner), gone(sleeper))
	}
	if out.String() != "started\n" {
		t.Errorf("stdout = %q, want what the program printed before the cancel", out.String())
	}
}

// The deadline is long enough for the scripts to start on a loaded machine:
// the first run of a new script can take hundreds of milliseconds on macOS.
func TestOSRunDeadlineStopsTheWholeProcessTree(t *testing.T) {
	tr := newTree(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- OS{}.Run(ctx, Command{Path: tr.outer}) }()
	inner, sleeper := tr.waitForPids(t)

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Run() error = %v, want one that wraps context.DeadlineExceeded", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run() did not return within 8 s of a 3 s deadline")
	}
	if !gone(inner) || !gone(sleeper) {
		t.Errorf("after the deadline: second script gone = %v, long-running program gone = %v; want both gone", gone(inner), gone(sleeper))
	}
}

// A program that ends on its own is not affected by the process group.
func TestOSRunInItsOwnGroupStillReportsTheExitStatus(t *testing.T) {
	var out bytes.Buffer
	err := OS{}.Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "sh -c 'printf nested'; exit 4"}, Stdout: &out})
	if ExitCode(err) != 4 || out.String() != "nested" {
		t.Errorf("Run() = %v with stdout %q, want exit status 4 and %q", err, out.String(), "nested")
	}
}
