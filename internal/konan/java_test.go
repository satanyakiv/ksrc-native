package konan

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func TestFindJavaFollowsRunKonan(t *testing.T) {
	javaHome := t.TempDir()
	inHome := filepath.Join(javaHome, "bin", "java")
	testutil.WriteFile(t, inHome, []byte("#!/bin/sh\n"), 0o755)
	notExecutable := t.TempDir()
	testutil.WriteFile(t, filepath.Join(notExecutable, "bin", "java"), []byte("not a program\n"), 0o644)

	onPath := &testutil.Runner{Paths: map[string]string{"java": "/usr/bin/java"}}
	nowhere := &testutil.Runner{}
	env := func(javaHome string) func(string) string {
		return func(key string) string {
			if key == "JAVA_HOME" {
				return javaHome
			}
			return ""
		}
	}

	tests := []struct {
		name     string
		javaHome string
		runner   executil.Runner
		want     string // the path; empty means an error
		message  string // part of the error
	}{
		{"JAVA_HOME with an executable java wins", javaHome, onPath, inHome, ""},
		{"JAVA_HOME with an executable java, nothing on PATH", javaHome, nowhere, inHome, ""},
		{"JAVA_HOME whose java is not executable: PATH", notExecutable, onPath, "/usr/bin/java", ""},
		{"JAVA_HOME without a java: PATH", t.TempDir(), onPath, "/usr/bin/java", ""},
		{"no JAVA_HOME: PATH", "", onPath, "/usr/bin/java", ""},
		{"no JAVA_HOME and nothing on PATH", "", nowhere, "", "no java on PATH and JAVA_HOME is not set"},
		{"a useless JAVA_HOME and nothing on PATH", notExecutable, nowhere, "", "no java on PATH and none at " + filepath.Join(notExecutable, "bin", "java")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindJava(env(tt.javaHome), tt.runner)
			if tt.want != "" {
				if got != tt.want || err != nil {
					t.Errorf("FindJava() = %q, %v; want %q", got, err, tt.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Errorf("FindJava() = %q, %v; want an error with %q", got, err, tt.message)
			}
		})
	}
}

func TestJavaVersion(t *testing.T) {
	ctx := context.Background()

	working := &testutil.Runner{RunFunc: testutil.FakeJava}
	version, err := JavaVersion(ctx, working, "/usr/bin/java")
	if err != nil || version != testutil.JavaVersionLine {
		t.Errorf("JavaVersion() = %q, %v; want the first line java prints, %q", version, err, testutil.JavaVersionLine)
	}
	if calls := working.Calls(); len(calls) != 1 || calls[0].Path != "/usr/bin/java" || strings.Join(calls[0].Args, " ") != "-version" {
		t.Errorf("JavaVersion() ran %+v, want one run of java -version", calls)
	}

	// /usr/bin/java on macOS exists and fails when no JDK is installed.
	stub := &testutil.Runner{RunFunc: testutil.NoJavaRuntime}
	_, err = JavaVersion(ctx, stub, "/usr/bin/java")
	if err == nil || !strings.Contains(err.Error(), "/usr/bin/java does not run: ") || !strings.Contains(err.Error(), "Unable to locate a Java Runtime") {
		t.Errorf("JavaVersion() with the macOS stub: error = %v, want one that says java does not run and why", err)
	}
	if err != nil && strings.Contains(err.Error(), "\n") {
		t.Errorf("JavaVersion() error = %q, want one line", err.Error())
	}

	// A java that prints nothing still counts as working.
	silent := &testutil.Runner{}
	if version, err := JavaVersion(ctx, silent, "/usr/bin/java"); err != nil || version != "version unknown" {
		t.Errorf("JavaVersion() with a silent java = %q, %v; want %q", version, err, "version unknown")
	}
}

// Other readers of the environment in this package (DataDir, Discover) take
// a nil function as "no variables"; FindJava must not be the one that panics.
func TestFindJavaWithoutAnEnvironment(t *testing.T) {
	onPath := &testutil.Runner{Paths: map[string]string{"java": "/usr/bin/java"}}
	got, err := FindJava(nil, onPath)
	if err != nil || got != "/usr/bin/java" {
		t.Errorf("FindJava(nil, java on PATH) = %q, %v; want /usr/bin/java", got, err)
	}
}
