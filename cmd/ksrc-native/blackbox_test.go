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

// The release binary gets its version from the linker flags of
// scripts/build.sh. A plain `go build` prints "dev", so only a build through
// the script can show that the flags name the right variable.
func TestBuildScriptStampsTheVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatalf("cannot read the VERSION file: %v", err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" || version == "dev" {
		t.Fatalf("the VERSION file holds %q, want a release version", version)
	}

	bin := filepath.Join(t.TempDir(), "ksrc-native")
	build := exec.Command(bash, filepath.Join(root, "scripts", "build.sh"), bin)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("scripts/build.sh: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version: %v", bin, err)
	}
	if got, want := string(out), "ksrc-native "+version+"\n"; got != want {
		t.Errorf("--version of a scripts/build.sh binary = %q, want %q (the VERSION file)", got, want)
	}
}

// A file-id is filled by a model, so its path part is untrusted. Whatever it
// holds, cat reads below the root of the module or reads nothing. The platform
// root has its cases in internal/adapter; these run the binary against the two
// roots that are read in place or extracted: the stdlib sources and the
// headers of an SDK.
func TestCatDoesNotLeaveTheSourceRoot(t *testing.T) {
	bin, env, project := buildBinary(t)

	xc := testutil.NewXcode(t)
	sdk := xc.AddSDK(t, "iPhoneOS", "26.4")
	xc.AddHeader(t, sdk, "UIKit", "UIView.h", "@interface UIView\n@end\n")
	programs := t.TempDir()
	xc.InstallPrograms(t, programs)
	env = append(env, "PATH="+programs+string(os.PathListSeparator)+os.Getenv("PATH"))

	// A file outside every root. If a path were followed as written, cat
	// would print it.
	const secretText = "TOP-SECRET-OUTSIDE-THE-ROOT"
	secret := filepath.Join(t.TempDir(), "secret.txt")
	testutil.WriteFile(t, secret, []byte(secretText+"\n"), 0o644)
	up := strings.Repeat("../", 40)

	roots := []struct {
		module string
		file   string // a file that exists, to show the root is readable
		want   string
	}{
		{"kn.stdlib:native", "nativeMain/kotlinx/cinterop/Utils.kt", "package kotlinx.cinterop"},
		{"sdk.apple:UIKit", "UIView.h", "@interface UIView"},
	}
	for _, root := range roots {
		code, stdout, stderr := run(t, bin, env, "--project", project, "cat", root.module+"!/"+root.file)
		if code != 0 || !strings.Contains(stdout, root.want) {
			t.Fatalf("cat %s!/%s: code = %d, stdout = %q, stderr = %q; want the file", root.module, root.file, code, stdout, stderr)
		}

		for name, inner := range map[string]string{
			"parent directories":           "/../../../../etc/passwd",
			"absolute path":                "//etc/hosts",
			"parents after a real file":    "/" + root.file + "/" + up + "etc/passwd",
			"parents up to a file we made": "/" + up + strings.TrimPrefix(secret, "/"),
			"absolute path of that file":   "/" + secret,
		} {
			t.Run(root.module+" "+name, func(t *testing.T) {
				id := root.module + "!" + inner
				code, stdout, stderr := run(t, bin, env, "--project", project, "cat", id)
				lines := strings.Split(strings.TrimSpace(stderr), "\n")
				if code != 1 || stdout != "" || !strings.HasPrefix(lines[len(lines)-1], "E_NO_FILE: ") {
					t.Errorf("cat %q: code = %d, stdout = %q, stderr = %q; want 1, nothing and E_NO_FILE", id, code, stdout, stderr)
				}
				if strings.Contains(stdout+stderr, secretText) {
					t.Errorf("cat %q printed a file outside the root:\n%s%s", id, stdout, stderr)
				}
			})
		}
	}
}

// A client that does not speak MCP ends the session. That is a failure of
// the server, with its own code, and stdout stays free of anything that is
// not the protocol.
func TestMCPGarbageOnStdinIsEMCP(t *testing.T) {
	bin, env, project := buildBinary(t)

	cmd := exec.Command(bin, "--project", project, "mcp")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("this is not a JSON-RPC message\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("mcp with garbage on stdin: error = %v, want exit code 1\nstderr: %s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("mcp with garbage on stdin wrote to stdout: %q", stdout.String())
	}
	if got := strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, "E_MCP: ") || !strings.Contains(got, ". Try: ") || strings.Contains(got, "\n") {
		t.Errorf("mcp with garbage on stdin: stderr = %q, want one line E_MCP with a hint", got)
	}
}
