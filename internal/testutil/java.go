package testutil

import (
	"context"
	"io"

	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// JavaVersionLine is the first line the fake java prints for -version.
const JavaVersionLine = "openjdk version \"17.0.9\" 2023-10-17"

// FakeJava is a java program for an Overlay. Like the real one, it prints
// its version to stderr.
func FakeJava(_ context.Context, cmd executil.Command) error {
	if len(cmd.Args) != 1 || cmd.Args[0] != "-version" {
		return &executil.ExitError{Path: cmd.Path, Code: 64, Stderr: "fake java: unsupported arguments"}
	}
	if cmd.Stderr != nil {
		io.WriteString(cmd.Stderr, JavaVersionLine+"\nOpenJDK Runtime Environment (build 17.0.9+9)\n")
	}
	return nil
}

// NoJavaRuntime is the java stub of a macOS without a JDK: the program
// exists and fails.
func NoJavaRuntime(_ context.Context, cmd executil.Command) error {
	return &executil.ExitError{Path: cmd.Path, Code: 1, Stderr: "The operation couldn\u2019t be completed. Unable to locate a Java Runtime.\nPlease visit http://www.java.com for information on installing Java.\n\n"}
}
