package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// buildBinary compiles the command into a temporary directory and prepares a
// fake home with one Kotlin/Native distribution and an empty cache. It
// returns the binary, the environment to run it in and an empty project
// directory.
func buildBinary(t *testing.T) (bin string, env []string, project string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short")
	}
	testutil.RequireRipgrep(t)
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}

	bin = filepath.Join(t.TempDir(), "ksrc-native")
	build := exec.Command(goTool, "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	home := t.TempDir()
	dist := testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.21")
	dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", testutil.ReadFixture(t, testutil.UserNotificationsDump))
	dist.AddStdlibSources(t, nil)
	env = append(os.Environ(),
		"HOME="+home,
		"KONAN_HOME=",
		"KONAN_DATA_DIR=",
		"KSRC_NATIVE_CACHE_DIR="+filepath.Join(t.TempDir(), "cache"),
	)
	return bin, env, t.TempDir()
}

// TestMCPOverStdio starts `ksrc-native mcp` as a process and talks to it
// with an MCP client over its stdin and stdout, the way an agent does.
func TestMCPOverStdio(t *testing.T) {
	bin, env, project := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	server := exec.Command(bin, "--project", project, "mcp")
	server.Env = env
	var serverStderr bytes.Buffer
	server.Stderr = &serverStderr
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: server}, nil)
	if err != nil {
		t.Fatalf("cannot connect to `ksrc-native mcp`: %v\nstderr: %s", err, serverStderr.String())
	}
	defer session.Close()

	if info := session.InitializeResult().ServerInfo; info.Name != "ksrc-native" {
		t.Errorf("server name = %q, want ksrc-native", info.Name)
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, " "); got != "cat deps search" {
		t.Errorf("tools/list = %q, want cat deps search", got)
	}

	const pattern = "val UNErrorDomain"
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": pattern, "modules": []string{"kn.platform:UserNotifications"}},
	})
	if err != nil || res.IsError || len(res.Content) == 0 {
		t.Fatalf("tools/call search: %v, result %+v\nstderr: %s", err, res, serverStderr.String())
	}
	viaMCP := res.Content[0].(*mcp.TextContent).Text

	cli := exec.Command(bin, "--project", project, "search", pattern, "--module", "kn.platform:UserNotifications")
	cli.Env = env
	viaCLI, err := cli.Output()
	if err != nil {
		t.Fatalf("search on the command line: %v", err)
	}
	want := "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:public final val UNErrorDomain: kotlin/String?\n"
	if viaMCP != string(viaCLI) || viaMCP != want {
		t.Errorf("MCP text   = %q\nCLI stdout = %q\nwant         %q", viaMCP, viaCLI, want)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": "x", "modules": []string{"kn.platform:Nope"}},
	})
	if err != nil || !res.IsError || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, "E_NO_SOURCES: ") {
		t.Errorf("tools/call with an unknown framework: %v, result %+v", err, res)
	}

	// A call cannot choose the distribution, and with it the klib program.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": "x", "modules": []string{"kn.platform:UserNotifications"}, "konan_home": project},
	})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "konan_home") {
		t.Errorf("tools/call with konan_home: %v, result %+v; want a validation error", err, res)
	}
	// Forbidden rg arguments fail the same way as on the command line.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": "x", "modules": []string{"kn.platform:UserNotifications"}, "rg_args": []string{"--pre", "/bin/cat"}},
	})
	if err != nil || !res.IsError || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, "E_BAD_ARGS: ") {
		t.Errorf("tools/call with rg_args --pre: %v, result %+v; want E_BAD_ARGS", err, res)
	}

	// Closing the session closes the server's stdin; the process exits with 0.
	if err := session.Close(); err != nil {
		t.Errorf("closing the session: %v\nstderr: %s", err, serverStderr.String())
	}
	if state := server.ProcessState; state == nil || state.ExitCode() != 0 {
		t.Errorf("the server process ended with %v, want exit code 0\nstderr: %s", state, serverStderr.String())
	}
}

// TestExitCodes builds the binary and runs it as a process: a shell script
// or an agent sees only the exit code, so every E_* error must end with a
// non-zero one. Tests of cli.Run cannot prove that, because main is not part
// of them.
func TestExitCodes(t *testing.T) {
	bin, env, project := buildBinary(t)

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
		{"search with allowed rg arguments", []string{"search", "VAL UNERRORDOMAIN", "--module", "kn.platform:UserNotifications", "--", "-i", "-g", "_toplevel.kt", "--max-count", "1"}, 0, ""},
		{"cat", []string{"cat", fileID, "--lines", "1,2"}, 0, ""},
		{"where", []string{"where", "kn.stdlib:native"}, 0, ""},
		{"clean without a terminal", []string{"clean"}, 0, ""},

		{"E_NO_SOURCES: unknown framework", []string{"search", "x", "--module", "kn.platform:Nope"}, 1, "E_NO_SOURCES: "},
		{"E_NO_SOURCES: unknown stdlib module", []string{"where", "kn.stdlib:jvm"}, 1, "E_NO_SOURCES: "},
		{"E_NO_KONAN: version that is not installed", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--kotlin-version", "1.9.0"}, 1, "E_NO_KONAN: "},
		{"E_NO_KONAN: bad --konan-home", []string{"deps", "--konan-home", filepath.Join(project, "nope")}, 1, "E_NO_KONAN: "},
		{"E_NO_SOURCES: target with a path", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--target", "../platform/ios_arm64"}, 1, "E_NO_SOURCES: "},
		{"E_NO_SOURCES: target in another case", []string{"where", "kn.platform:UserNotifications", "--target", "IOS_ARM64"}, 1, "E_NO_SOURCES: "},
		{"E_BAD_ARGS: SDK that is a path", []string{"where", "sdk.apple:UIKit", "--sdk", "/tmp"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_COORD", []string{"search", "x", "--module", "com.example:lib"}, 1, "E_BAD_COORD: "},
		{"E_BAD_FILE_ID", []string{"cat", "UNNotification.kt"}, 1, "E_BAD_FILE_ID: "},
		{"E_NO_FILE", []string{"cat", "kn.platform:UserNotifications:2.4.21!/ios_arm64/Nope.kt"}, 1, "E_NO_FILE: "},
		{"E_BAD_LINES", []string{"cat", fileID, "--lines", "x"}, 1, "E_BAD_LINES: "},
		{"E_BAD_ARGS: unknown command", []string{"frobnicate"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: unknown flag", []string{"search", "x", "--nope"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: unknown MCP tool", []string{"mcp", "--tools=clean"}, 1, "E_BAD_ARGS: "},
		{"E_RG_FAILED: bad regular expression", []string{"search", "(", "--module", "kn.platform:UserNotifications"}, 1, "E_RG_FAILED: "},
		{"E_BAD_ARGS: pattern that starts with a dash is not passed on", []string{"search", "--module", "kn.platform:UserNotifications", "--files"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: rg preprocessor", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--", "--pre", "/bin/cat"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: rg path", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--", "/etc/hosts"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: rg output mode", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--", "-l"}, 1, "E_BAD_ARGS: "},
		{"E_BAD_ARGS: rg flag without its value", []string{"search", "x", "--module", "kn.platform:UserNotifications", "--", "-g"}, 1, "E_BAD_ARGS: "},
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

// run executes the binary with extra environment variables and returns its
// exit code, stdout and stderr.
func run(t *testing.T, bin string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), stdout.String(), stderr.String()
	case err != nil:
		t.Fatalf("cannot run the binary: %v", err)
	}
	return 0, stdout.String(), stderr.String()
}

// A KONAN_HOME that is set and wrong must not hide behind the SDK list.
func TestDepsWithAKonanHomeVariableThatCannotBeUsed(t *testing.T) {
	bin, env, project := buildBinary(t)
	nope := filepath.Join(project, "nope")
	code, stdout, stderr := run(t, bin, append(env, "KONAN_HOME="+nope), "--project", project, "deps")
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "E_NO_KONAN: KONAN_HOME points at "+nope) {
		t.Errorf("deps with KONAN_HOME=%s: code = %d, stdout = %q, stderr = %q; want 1, nothing and E_NO_KONAN that names the variable", nope, code, stdout, stderr)
	}
}
