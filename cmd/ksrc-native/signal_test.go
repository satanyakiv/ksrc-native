//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// TestSignalStopsTheCommandAndKlib sends the signals a terminal and a process
// manager send while klib builds a dump. The command must end at once with
// E_CANCELLED and take the processes klib started with it.
func TestSignalStopsTheCommandAndKlib(t *testing.T) {
	bin, env, project := buildBinary(t)

	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			dist := testutil.NewDistribution(t, t.TempDir(), "2.4.21")
			dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", testutil.ReadFixture(t, testutil.UserNotificationsDump))
			slow := dist.MakeKlibSlow(t)

			cmd := exec.Command(bin, "--project", project, "--konan-home", dist.Home,
				"search", "x", "--module", "kn.platform:UserNotifications")
			cmd.Env = env
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatalf("cannot start the binary: %v", err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { cmd.Process.Kill() })

			sleeper := slow.WaitStarted(t)
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatalf("cannot send %s: %v", sig, err)
			}

			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("the command still runs 5 s after %s\nstderr: %s", sig, stderr.String())
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Errorf("the command ended with %v, want exit code 1\nstderr: %s", err, stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			if last := lines[len(lines)-1]; !strings.HasPrefix(last, "E_CANCELLED: ") || !strings.Contains(last, "klib") {
				t.Errorf("last stderr line = %q, want E_CANCELLED that names klib", last)
			}
			if stdout.Len() != 0 {
				t.Errorf("an interrupted command wrote to stdout: %q", stdout.String())
			}
			if !testutil.ProcessGone(sleeper, 3*time.Second) {
				t.Errorf("the program klib started (pid %d) still runs after %s", sleeper, sig)
			}
			entries, _ := os.ReadDir(envValue(env, "KSRC_NATIVE_CACHE_DIR") + "/platform-dumps-v1")
			for _, e := range entries {
				t.Errorf("the cache holds %s after an interrupted build, want nothing", e.Name())
			}
		})
	}
}

// envValue returns the last value of a variable in an environment list.
func envValue(env []string, name string) string {
	value := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			value = v
		}
	}
	return value
}
