package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// repoFile returns the path of a file of the repository.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// The installer is run as `curl ... | sh`. A download that stops in the
// middle hands the shell the first part of the script, and the shell runs
// what it got. The whole body is one function that the last line calls, so a
// script that was cut does nothing.
func TestInstallScriptCutShortRunsNothing(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	data, err := os.ReadFile(repoFile(t, "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1] + lines[len(lines)-2]); last != `main "$@"` {
		t.Errorf("the last line of install.sh is %q, want %q", last, `main "$@"`)
	}

	// run feeds a script to sh the way a pipe does and reports whether it
	// reached the network (a fake curl leaves a marker) or the install prefix.
	run := func(script string) (ranCurl, wroteToPrefix bool) {
		dir := t.TempDir()
		marker := filepath.Join(dir, "curl-was-run")
		bin := filepath.Join(dir, "bin")
		testutil.WriteFile(t, filepath.Join(bin, "curl"), []byte(fmt.Sprintf("#!/bin/sh\necho run > '%s'\nexit 22\n", marker)), 0o755)
		prefix := filepath.Join(dir, "prefix")

		cmd := exec.Command(sh)
		cmd.Stdin = strings.NewReader(script)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"HOME="+filepath.Join(dir, "home"),
			"KSRC_NATIVE_PREFIX="+prefix,
			"KSRC_NATIVE_VERSION=",
			"KSRC_NATIVE_BASE_URL=",
		)
		_ = cmd.Run() // a cut script may end with a syntax error; that is fine
		_, curlErr := os.Stat(marker)
		_, prefixErr := os.Stat(prefix)
		return curlErr == nil, prefixErr == nil
	}

	// The whole script does reach the network, so the marker works.
	if ranCurl, _ := run(string(data)); !ranCurl {
		t.Fatal("the complete install.sh did not run curl: the test cannot tell a script that ran from one that did not")
	}
	for n := 1; n < len(lines)-1; n++ {
		ranCurl, wroteToPrefix := run(strings.Join(lines[:n], ""))
		if ranCurl || wroteToPrefix {
			t.Errorf("install.sh cut after line %d (%q) still ran: curl = %v, wrote to the prefix = %v", n, strings.TrimSpace(lines[n-1]), ranCurl, wroteToPrefix)
		}
	}
	// A cut in the middle of the last line, where the call is.
	for _, tail := range []string{"mai", "main", `main "$`} {
		if ranCurl, _ := run(strings.Join(lines[:len(lines)-2], "") + tail); ranCurl && tail != "main" {
			t.Errorf("install.sh that ends with %q ran curl", tail)
		}
	}
}
