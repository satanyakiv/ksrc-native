package konan

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// A JVM that fails prints a stack trace of hundreds of lines. The error
// keeps the start of it on one line.
func TestDumpMetadataFailureIsOneShortLine(t *testing.T) {
	dist, _ := fakeDistribution(t)
	klib, err := dist.FindPlatformKlib("ios_arm64", "UIKit")
	if err != nil {
		t.Fatal(err)
	}
	trace := "Exception in thread \"main\" java.lang.IllegalStateException: no metadata\n" +
		strings.Repeat("\tat org.jetbrains.kotlin.Some.frame(Some.kt:42)\n", 300)
	runner := &testutil.Runner{RunFunc: func(context.Context, executil.Command) error {
		return &executil.ExitError{Path: dist.KlibTool(), Code: 1, Stderr: trace}
	}}

	err = dist.DumpMetadata(context.Background(), runner, klib.Path, &bytes.Buffer{})
	if errs.Code(err) != errs.KlibFailed {
		t.Fatalf("DumpMetadata() error = %v, want %s", err, errs.KlibFailed)
	}
	text := err.Error()
	if strings.Contains(text, "\n") || strings.Contains(text, "\t") {
		t.Errorf("the error has several lines:\n%s", text)
	}
	if !strings.Contains(text, "exit status 1") || !strings.Contains(text, "java.lang.IllegalStateException: no metadata at org.jetbrains.kotlin.Some.frame") {
		t.Errorf("the error %q should carry the exit status and the start of what klib printed", text)
	}
	// The path of the library, about 300 bytes of klib's output, the hint.
	if limit := len(klib.Path) + 600; len(text) > limit {
		t.Errorf("the error has %d bytes, want at most %d:\n%s", len(text), limit, text)
	}
	if !strings.Contains(text, "Try: run `ksrc-native doctor`") || !strings.Contains(text, "java line") {
		t.Errorf("the error %q should send the user to the java line of `ksrc-native doctor`", text)
	}
}
