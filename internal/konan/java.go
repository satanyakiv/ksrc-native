package konan

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// javaMessageBytes bounds what a failing java contributes to a message.
const javaMessageBytes = 300

// FindJava returns the java program the klib tool will run. It follows
// <konanHome>/bin/run_konan: $JAVA_HOME/bin/java when that file is
// executable, else java on PATH.
func FindJava(getenv func(string) string, runner executil.Runner) (string, error) {
	inHome := ""
	home := ""
	if getenv != nil {
		home = getenv("JAVA_HOME")
	}
	if home != "" {
		inHome = filepath.Join(home, "bin", "java")
		if info, err := os.Stat(inHome); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return inHome, nil
		}
	}
	if path, err := runner.LookPath("java"); err == nil {
		return path, nil
	}
	if inHome == "" {
		return "", fmt.Errorf("no java on PATH and JAVA_HOME is not set")
	}
	return "", fmt.Errorf("no java on PATH and none at %s (JAVA_HOME)", inHome)
}

// JavaVersion runs "java -version" and returns the first line it prints.
// Finding the program is not enough: /usr/bin/java on macOS is a stub that
// exists and fails when no JDK is installed.
func JavaVersion(ctx context.Context, runner executil.Runner, java string) (string, error) {
	// java prints its version to stderr. The two streams get a buffer each:
	// they are filled by two goroutines.
	var stdout, stderr bytes.Buffer
	if err := runner.Run(ctx, executil.Command{Path: java, Args: []string{"-version"}, Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", fmt.Errorf("%s does not run: %s", java, executil.Summary(err, javaMessageBytes))
	}
	text := strings.TrimSpace(stderr.String())
	if text == "" {
		text = strings.TrimSpace(stdout.String())
	}
	first, _, _ := strings.Cut(text, "\n")
	if first = strings.TrimSpace(first); first != "" {
		return first, nil
	}
	return "version unknown", nil
}
