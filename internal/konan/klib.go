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
// the dump to w. The tool runs on the JVM, so it needs a JDK on PATH.
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
	if err != nil {
		return errs.Wrap(err, errs.KlibFailed,
			fmt.Sprintf("klib dump-metadata failed for %s: %v", klibDir, err),
			"check that a JDK is on PATH (the klib tool runs on the JVM) and run `ksrc-native doctor`")
	}
	return nil
}
