package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func TestEveryProgramHasADeadline(t *testing.T) {
	tests := []struct {
		program string
		want    time.Duration
	}{
		{"klib", 120 * time.Second},
		{"rg", 60 * time.Second},
		{"xcrun", 15 * time.Second},
		{"xcode-select", 15 * time.Second},
		{"something-new", 60 * time.Second},
	}
	for _, tt := range tests {
		if got := deadlineOf(tt.program); got != tt.want {
			t.Errorf("deadlineOf(%q) = %s, want %s", tt.program, got, tt.want)
		}
	}
}

// blocking is a program that runs until its context ends.
func blocking(ctx context.Context, _ executil.Command) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestLimitedRunner(t *testing.T) {
	short := func(string) time.Duration { return 50 * time.Millisecond }
	long := func(string) time.Duration { return time.Minute }

	t.Run("a program past its deadline", func(t *testing.T) {
		runner := limitedRunner{inner: &testutil.Runner{RunFunc: blocking}, limit: short}
		err := runner.Run(context.Background(), executil.Command{Path: "/konan/bin/klib"})
		if errs.Code(err) != errs.Timeout || !strings.Contains(err.Error(), "klib did not finish within 50ms") {
			t.Errorf("Run() error = %v, want %s that names the program and the limit", err, errs.Timeout)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Run() error = %v, want one that wraps context.DeadlineExceeded", err)
		}
	})

	t.Run("a cancelled run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		runner := limitedRunner{inner: &testutil.Runner{RunFunc: blocking}, limit: long}
		err := runner.Run(ctx, executil.Command{Path: "/usr/bin/rg"})
		if errs.Code(err) != errs.Cancelled || !strings.Contains(err.Error(), "rg") {
			t.Errorf("Run() error = %v, want %s that names the program", err, errs.Cancelled)
		}
	})

	t.Run("a program that fails on its own", func(t *testing.T) {
		exit := &executil.ExitError{Path: "rg", Code: 2, Stderr: "regex parse error"}
		runner := limitedRunner{inner: &testutil.Runner{RunFunc: func(context.Context, executil.Command) error { return exit }}, limit: long}
		if err := runner.Run(context.Background(), executil.Command{Path: "rg"}); err != error(exit) {
			t.Errorf("Run() error = %v, want the exit error unchanged", err)
		}
	})

	t.Run("a program that succeeds", func(t *testing.T) {
		inner := &testutil.Runner{Paths: map[string]string{"rg": "/usr/bin/rg"}}
		runner := limitedRunner{inner: inner, limit: long}
		if err := runner.Run(context.Background(), executil.Command{Path: "rg"}); err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
		if path, err := runner.LookPath("rg"); path != "/usr/bin/rg" || err != nil {
			t.Errorf("LookPath(rg) = %q, %v; want the answer of the inner runner", path, err)
		}
	})
}

// leftovers lists what is in the platform namespace of the cache.
func leftovers(t *testing.T, w *world) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(w.env[cache.EnvDir], "platform-dumps-v1"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestACancelStopsKlibAndEverythingItStarted(t *testing.T) {
	w := newWorld(t)
	slow := w.dist.MakeKlibSlow(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}) }()
	sleeper := slow.WaitStarted(t)
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Search() did not return within 5 s of the cancel")
	}
	if errs.Code(err) != errs.Cancelled || !strings.Contains(err.Error(), "klib") {
		t.Errorf("Search() error = %v, want %s that names klib", err, errs.Cancelled)
	}
	if strings.Contains(err.Error(), "JDK") || strings.Contains(err.Error(), errs.KlibFailed) {
		t.Errorf("Search() error = %v: a cancelled run is not a klib failure", err)
	}
	if !testutil.ProcessGone(sleeper, 3*time.Second) {
		t.Errorf("the program klib started (pid %d) still runs after the cancel", sleeper)
	}
	if left := leftovers(t, w); len(left) != 0 {
		t.Errorf("the cache holds %q after a cancelled build, want nothing", left)
	}
}

// The deadline is long enough for the scripts to start on a loaded machine.
func TestKlibPastItsDeadlineIsStopped(t *testing.T) {
	w := newWorld(t)
	slow := w.dist.MakeKlibSlow(t)
	w.a.limit = func(program string) time.Duration {
		if program == "klib" {
			return 3 * time.Second
		}
		return deadlineOf(program)
	}

	done := make(chan error, 1)
	go func() {
		done <- w.a.Search(context.Background(), SearchRequest{Pattern: "x", Modules: []string{unModule}})
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("Search() did not return within 8 s of a 3 s deadline")
	}
	if errs.Code(err) != errs.Timeout || !strings.Contains(err.Error(), "klib did not finish within 3s") {
		t.Errorf("Search() error = %v, want %s that names klib and the limit", err, errs.Timeout)
	}
	if !strings.Contains(err.Error(), "ksrc-native doctor") {
		t.Errorf("Search() error = %v, want a hint that names `ksrc-native doctor`", err)
	}
	if sleeper := slow.WaitStarted(t); !testutil.ProcessGone(sleeper, 3*time.Second) {
		t.Errorf("the program klib started (pid %d) still runs after the deadline", sleeper)
	}
	if left := leftovers(t, w); len(left) != 0 {
		t.Errorf("the cache holds %q after a stopped build, want nothing", left)
	}
}
