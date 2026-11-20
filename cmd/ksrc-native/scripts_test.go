package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

// needBash skips a test that runs a script of the repository.
func needBash(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs a script of the repository; skipped with -short")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	return bash
}

// THIRD_PARTY_NOTICES is committed, and it is generated from the modules the
// binary links. A new or raised dependency without a new file would ship an
// archive that lacks a notice its licence asks for.
func TestThirdPartyNoticesAreCurrent(t *testing.T) {
	bash := needBash(t)
	out, err := exec.Command(bash, repoFile(t, "scripts", "notices.sh"), "--check").CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/notices.sh --check: %v\n%s\nrun scripts/notices.sh and commit THIRD_PARTY_NOTICES", err, out)
	}

	data, err := os.ReadFile(repoFile(t, "THIRD_PARTY_NOTICES"))
	if err != nil {
		t.Fatal(err)
	}
	// Every module of the build of this test binary is linked into the tool
	// as well, and the file must name each with its version.
	list := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", ".")
	list.Dir = repoFile(t, "cmd", "ksrc-native")
	modules, err := list.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	seen := 0
	for _, module := range strings.Split(strings.TrimSpace(string(modules)), "\n") {
		if module == "" {
			continue
		}
		seen++
		if !strings.Contains(string(data), "\n"+module+"\n") {
			t.Errorf("THIRD_PARTY_NOTICES has no section for %s", module)
		}
	}
	if seen == 0 {
		t.Error("go list named no module; the check above checked nothing")
	}
	for _, part := range []string{"Go standard library and runtime", "Redistribution and use in source and binary forms", "Apache License"} {
		if !strings.Contains(string(data), part) {
			t.Errorf("THIRD_PARTY_NOTICES lacks %q", part)
		}
	}
}

func TestPackageScriptPutsTheNoticesInTheArchive(t *testing.T) {
	bash := needBash(t)
	dist := t.TempDir()
	cmd := exec.Command(bash, repoFile(t, "scripts", "package.sh"), dist)
	// One archive is enough to see what goes into each.
	cmd.Env = append(os.Environ(), "KSRC_NATIVE_TARGETS="+runtime.GOOS+"/"+runtime.GOARCH)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scripts/package.sh: %v\n%s", err, out)
	}
	version, err := os.ReadFile(repoFile(t, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ksrc-native_%s_%s_%s.tar.gz", strings.TrimSpace(string(version)), runtime.GOOS, runtime.GOARCH)

	f, err := os.Open(filepath.Join(dist, name))
	if err != nil {
		t.Fatalf("package.sh wrote no %s: %v", name, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	content := make(map[string][]byte)
	for tr := tar.NewReader(gz); ; {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if header.Name != "ksrc-native" {
			if content[header.Name], err = io.ReadAll(tr); err != nil {
				t.Fatal(err)
			}
		}
	}
	sort.Strings(names)
	if got, want := strings.Join(names, " "), "LICENSE.txt README.md THIRD_PARTY_NOTICES ksrc-native"; got != want {
		t.Errorf("%s holds %q, want %q", name, got, want)
	}
	committed, err := os.ReadFile(repoFile(t, "THIRD_PARTY_NOTICES"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content["THIRD_PARTY_NOTICES"]) != string(committed) {
		t.Error("THIRD_PARTY_NOTICES in the archive differs from the file in the repository")
	}
	sums, err := os.ReadFile(filepath.Join(dist, "checksums.txt"))
	if err != nil || !strings.Contains(string(sums), name) {
		t.Errorf("checksums.txt = %q (%v), want an entry for %s", sums, err, name)
	}
}
