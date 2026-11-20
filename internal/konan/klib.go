package konan

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// DumpMetadata runs `<konanHome>/bin/klib dump-metadata <klibDir>` and writes
// the dump to w. The tool runs on the JVM, so it needs a JDK; see FindJava.
func (d Distribution) DumpMetadata(ctx context.Context, runner executil.Runner, klibDir string, w io.Writer) error {
	tool := d.KlibTool()
	if _, err := os.Stat(tool); err != nil {
		return errs.New(errs.NoKonan,
			fmt.Sprintf("the klib tool is missing at %s", tool),
			"reinstall the distribution: delete "+d.Home+" and run a Kotlin/Native Gradle task once")
	}

	err := runner.Run(ctx, executil.Command{
		Path:   tool,
		Args:   []string{"dump-metadata", klibDir},
		Stdout: w,
	})
	if errs.Stopped(err) {
		// The tool stopped klib itself; that says nothing about klib.
		return err
	}
	if err != nil {
		status := ""
		if code := executil.ExitCode(err); code >= 0 {
			status = fmt.Sprintf(" (exit status %d)", code)
		}
		return errs.Wrap(err, errs.KlibFailed,
			fmt.Sprintf("klib dump-metadata failed for %s%s: %s", klibDir, status, executil.Summary(err, klibMessageBytes)),
			"run `ksrc-native doctor` and read its java line: the klib tool runs on a JDK")
	}
	return nil
}

// klibMessageBytes bounds what klib's own output contributes to an error
// line. A JVM that fails prints a stack trace of hundreds of lines.
const klibMessageBytes = 300
