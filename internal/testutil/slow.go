package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SlowKlib is a klib tool that never finishes on its own.
type SlowKlib struct {
	pidFile string
}

// MakeKlibSlow replaces the klib tool of the distribution with one that runs
// until it is killed. It is shaped like the real tool: a script that starts
// a second script, which starts the long-running program, and neither uses
// exec. Killing the first process alone leaves the other two running.
func (d Distribution) MakeKlibSlow(tb testing.TB) *SlowKlib {
	tb.Helper()
	slow := &SlowKlib{pidFile: filepath.Join(d.Home, "klib-sleeper.pid")}
	inner := filepath.Join(d.Home, "bin", "klib-inner")
	WriteFile(tb, filepath.Join(d.Home, "bin", "klib"), []byte("#!/bin/sh\n'"+inner+"'\n"), 0o755)
	WriteFile(tb, inner, []byte("#!/bin/sh\nsleep 60 &\necho $! > '"+slow.pidFile+"'\nwait\n"), 0o755)
	tb.Cleanup(func() {
		if pid, ok := slow.pid(); ok {
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
	})
	return slow
}

func (s *SlowKlib) pid() (int, bool) {
	data, err := os.ReadFile(s.pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, err == nil && pid > 1
}

// WaitStarted waits until the long-running program at the bottom of the tool
// runs, and returns its pid.
func (s *SlowKlib) WaitStarted(tb testing.TB) int {
	tb.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := s.pid(); ok {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	tb.Fatal("testutil: the slow klib tool did not start within 10 s")
	return 0
}

// ProcessGone reports whether the process has ended, waiting up to the given
// time for it.
func ProcessGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		p, err := os.FindProcess(pid)
		if err != nil {
			return true
		}
		if err := p.Signal(syscall.Signal(0)); err != nil && !errors.Is(err, syscall.EPERM) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
