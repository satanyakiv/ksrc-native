package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// TestExitCodes builds the binary and runs it as a process: a shell script
// or an agent sees only the exit code, so every E_* error must end with a
// non-zero one. Tests of cli.Run cannot prove that, because main is not part
// of them.
func TestExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short")
	}
	testutil.RequireRipgrep(t)
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}

	bin := filepath.Join(t.TempDir(), "ksrc-native")
	build := exec.Command(goTool, "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	home := t.TempDir()
	dist := testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.21")
	dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", testutil.ReadFixture(t, testutil.UserNotificationsDump))
	dist.AddStdlibSources(t, nil)
	project := t.TempDir()
	env := append(os.Environ(),
		"HOME="+home,
		"KONAN_HOME=",
		"KONAN_DATA_DIR=",
		"KSRC_NATIVE_CACHE_DIR="+filepath.Join(t.TempDir(), "cache"),
	)

	const fileID = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt"
	tests := []struct {
		name string
		args []string
		code int
		// stderr is the prefix of the last stderr line; empty means no check.
		stderr string
	}{
		{"version", []string{"--version"}, 0, ""},
		{"help", []string{"--help"}, 0, ""},
		{"search with a match", []string{"search", "val UNErrorDomain", "--module", "kn.platform:UserNotifications"}, 0, ""},
		{"search without a match", []string{"search", "NoSuchSymbolAnywhere", "--module", "kn.platform:UserNotifications"}, 0, "ksrc-native: no matches"},
		{"cat", []string{"cat", fileID, "--lines", "1,2"}, 0, ""},
		{"where", []string{"where", "kn.stdlib:native"}, 0, ""},
		{"clean without a terminal", []string{"clean"}, 0, ""},

		{"E_NO_SOURCES: unknown framework", []string{"search", "x", "--module", "kn.platform:Nope"}, 1, "E_NO_SOURCES: "},
		{"E_NO_SOURCES: unknown stdlib module", []string{"where", "kn.stdlib:jvm"}, 1, "E_NO_SOURCES: "},
		{"E_NO_KONAN: version that is not installed", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--kotlin-version", "1.9.0"}, 1, "E_NO_KONAN: "},
		{"E_NO_KONAN: bad --konan-home", []string{"deps", "--konan-home", filepath.Join(home, "nope")}, 1, "E_NO_KONAN: "},
		{"E_BAD_COORD", []string{"search", "x", "--module", "com.example:lib"}, 1, "E_BAD_COORD: "},
		{"E_BAD_FILE_ID", []string{"cat", "UNNotification.kt"}, 1, "E_BAD_FILE_ID: "},
		{"E_NO_FILE", []string{"cat", "kn.platform:UserNotifications:2.4.21!/ios_arm64/Nope.kt"}, 1, "E_NO_FILE: "},
		{"E_BAD_LINES", []string{"cat", fileID, "--lines", "x"}, 1, "E_BAD_LINES: "},
		{"E_BAD_ARGS: unknown command", []string{"frobnicate"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: unknown flag", []string{"search", "x", "--nope"}, 1, "E_BAD_ARGS: "},
		{"E_RG_FAILED: bad regular expression", []string{"search", "(", "--module", "kn.platform:UserNotifications"}, 1, "E_RG_FAILED: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, append([]string{"--project", project}, tt.args...)...)
			cmd.Env = env
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()

			code := 0
			var exit *exec.ExitError
			switch {
			case errors.As(err, &exit):
				code = exit.ExitCode()
			case err != nil:
				t.Fatalf("cannot run the binary: %v", err)
			}
			if code != tt.code {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, tt.code, stdout.String(), stderr.String())
			}
			if tt.code != 0 && stdout.Len() != 0 {
				t.Errorf("a failing command wrote to stdout: %q", stdout.String())
			}
			if tt.stderr != "" {
				lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
				if last := lines[len(lines)-1]; !strings.HasPrefix(last, tt.stderr) {
					t.Errorf("last stderr line = %q, want prefix %q", last, tt.stderr)
				}
			}
		})
	}
}
