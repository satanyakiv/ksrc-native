package adapter

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// How long each program the tool starts may run. A program that runs longer
// is stopped together with everything it started, and the operation fails
// with E_TIMEOUT. The numbers are several times the slowest run seen: the
// dump of the largest framework takes about 20 s on a cold JVM, a search of
// every cached dump about a second, and xcrun a few seconds when it rebuilds
// its own cache.
const (
	klibDeadline    = 120 * time.Second
	rgDeadline      = 60 * time.Second
	xcodeDeadline   = 15 * time.Second // xcrun and xcode-select
	javaDeadline    = 15 * time.Second // java -version, which doctor runs
	defaultDeadline = 60 * time.Second // a program without a line of its own
)

// deadlineOf returns the deadline of a program by its file name.
func deadlineOf(program string) time.Duration {
	switch program {
	case "klib":
		return klibDeadline
	case "rg":
		return rgDeadline
	case "xcrun", "xcode-select":
		return xcodeDeadline
	case "java":
		return javaDeadline
	}
	return defaultDeadline
}

// timeoutHint says what to do after a program was stopped at its deadline.
func timeoutHint(program string) string {
	switch program {
	case "klib":
		return "run the command again; if klib is stopped again, run `ksrc-native doctor` and check the JDK"
	case "rg":
		return "narrow the search with --module or a more specific pattern"
	case "xcrun", "xcode-select":
		return "run `xcrun --sdk iphoneos --show-sdk-path` in a terminal to see why it does not answer"
	}
	return "run the command again"
}

// runner returns the Runner every operation uses: the adapter's Runner with
// a deadline per program.
func (a *Adapter) runner() executil.Runner {
	limit := a.limit
	if limit == nil {
		limit = deadlineOf
	}
	return limitedRunner{inner: a.Runner, limit: limit}
}

// limitedRunner gives every run a deadline and turns a run that was stopped,
// by the deadline or by a cancel of the caller, into a coded error. Without
// it such a run looks like a failure of the program, and the hint sends the
// user to look for a JDK or to fix a pattern.
type limitedRunner struct {
	inner executil.Runner
	limit func(program string) time.Duration
}

// LookPath implements executil.Runner.
func (l limitedRunner) LookPath(name string) (string, error) { return l.inner.LookPath(name) }

// Run implements executil.Runner.
func (l limitedRunner) Run(ctx context.Context, cmd executil.Command) error {
	program := filepath.Base(cmd.Path)
	limit := l.limit(program)
	limited, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	err := l.inner.Run(limited, cmd)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return errs.Wrap(err, errs.Cancelled,
			fmt.Sprintf("the run was cancelled while %s was running", program),
			"run the command again")
	case limited.Err() != nil:
		return errs.Wrap(err, errs.Timeout,
			fmt.Sprintf("%s did not finish within %s and was stopped", program, limit),
			timeoutHint(program))
	}
	return err
}
