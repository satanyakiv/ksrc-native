// Package cli defines the ksrc-native command line.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/konan"
)

// Version is the tool version. scripts/build.sh injects the content of the
// VERSION file; a plain `go build` leaves "dev".
var Version = "dev"

// Main runs the command line against the real environment and returns the
// process exit code.
func Main() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, os.Args[1:], adapter.New(os.Stdout, os.Stderr))
}

// Run executes one command line. Results go to a.Stdout; errors are printed
// to a.Stderr as "E_CODE: message. Try: hint". It returns 0 on success and 1
// on any error.
func Run(ctx context.Context, args []string, a *adapter.Adapter) int {
	root := newRoot(a)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(a.Stderr, describe(err))
		return 1
	}
	return 0
}

// describe renders an error for stderr. Errors that cobra produces itself
// (unknown command, unknown flag) carry no code and get E_BAD_ARGS.
func describe(err error) string {
	if errs.Code(err) != "" {
		return err.Error()
	}
	return usageError(err.Error()).Error()
}

func usageError(message string) error {
	return errs.New(errs.BadArgs, message, "run `ksrc-native --help` to see the commands and flags")
}

// common binds the flags shared by all commands.
type common struct {
	konanHome     string
	kotlinVersion string
	project       string
	target        string
}

func (c *common) request() adapter.Common {
	return adapter.Common{
		KonanHome:     c.konanHome,
		KotlinVersion: c.kotlinVersion,
		Project:       c.project,
		Target:        c.target,
	}
}

func newRoot(a *adapter.Adapter) *cobra.Command {
	flags := &common{}
	root := &cobra.Command{
		Use:   "ksrc-native",
		Short: "Search Kotlin/Native platform APIs (platform.UIKit, platform.Foundation, ...)",
		Long: `ksrc-native searches and reads the Kotlin signatures of Kotlin/Native
platform libraries: the declarations behind "import platform.UIKit.*".

Platform libraries ship as klibs without sources, so the tool dumps their
metadata once, splits it into one file per class and caches the result.

Typical flow:
  ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
  ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' --lines 1,40`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	root.SetVersionTemplate("ksrc-native {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err.Error())
	})
	root.CompletionOptions.DisableDefaultCmd = true

	pf := root.PersistentFlags()
	pf.StringVar(&flags.konanHome, "konan-home", "", "path of the Kotlin/Native distribution (default: discovered, see `doctor`)")
	pf.StringVar(&flags.kotlinVersion, "kotlin-version", "", "Kotlin version of the distribution to use, for example 2.4.21")
	pf.StringVar(&flags.project, "project", ".", "project directory; the Kotlin version is read from its gradle/libs.versions.toml")
	pf.StringVar(&flags.target, "target", konan.DefaultTarget, "Kotlin/Native target of the platform libraries")

	root.AddCommand(newSearch(a, flags), newCat(a, flags), newDoctor(a, flags))
	return root
}

func newSearch(a *adapter.Adapter, flags *common) *cobra.Command {
	var (
		modules      []string
		contextLines int
	)
	cmd := &cobra.Command{
		Use:     "search <pattern> [--module <coordinate>]... [-- <rg-args>]",
		Aliases: []string{"rg"},
		Short:   "Search platform library signatures with a regular expression",
		Long: `Search runs ripgrep over the signatures of the named platform libraries and
prints one line per match:

  <file-id> <line>:<col>:<text>

A module is kn.platform:<Framework>[:<kotlinVersion>]. The first search of a
framework builds its dump (a few seconds); later searches read the cache.
Without --module the libraries that are already cached are searched.

Names in a dump are fully qualified with slashes: platform/UIKit/UIView,
kotlin/String. Arguments after "--" go to rg, for example "-- -i" or "-- -F".`,
		Example: `  ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
  ksrc-native search 'fun dataTaskWithURL' --module kn.platform:Foundation --context 2
  ksrc-native search 'NSURLErrorDomain' --module kn.platform:Foundation -- -F`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern, rgArgs, err := splitSearchArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			return a.Search(cmd.Context(), adapter.SearchRequest{
				Common:  flags.request(),
				Pattern: pattern,
				Modules: modules,
				Context: contextLines,
				RgArgs:  rgArgs,
			})
		},
	}
	cmd.Flags().StringArrayVarP(&modules, "module", "m", nil, "module to search, kn.platform:<Framework>[:<kotlinVersion>]; repeatable")
	cmd.Flags().IntVarP(&contextLines, "context", "C", 0, "lines of context around each match")
	return cmd
}

// splitSearchArgs separates the pattern from the rg arguments that follow
// "--". dash is the number of arguments before "--", or -1 without one.
func splitSearchArgs(args []string, dash int) (pattern string, rgArgs []string, err error) {
	own := args
	if dash >= 0 {
		own, rgArgs = args[:dash], args[dash:]
	}
	if len(own) != 1 {
		return "", nil, errs.New(errs.BadArgs,
			fmt.Sprintf("search takes one pattern, got %d arguments", len(own)),
			"quote the pattern and put rg arguments after `--`: ksrc-native search '<pattern>' --module kn.platform:UIKit -- -i")
	}
	return own[0], rgArgs, nil
}

func newCat(a *adapter.Adapter, flags *common) *cobra.Command {
	var (
		lines   string
		numbers bool
	)
	cmd := &cobra.Command{
		Use:     "cat <file-id> [--lines A,B]",
		Aliases: []string{"open"},
		Short:   "Print a file named by a file-id from search output",
		Example: `  ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' --lines 1,40
  ksrc-native cat 'kn.platform:Foundation:2.4.21!/ios_arm64/_toplevel.kt' -n --lines 100,120`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.New(errs.BadArgs,
					fmt.Sprintf("cat takes one file-id, got %d arguments", len(args)),
					"quote the file-id, it contains `!`: ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt'")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Cat(cmd.Context(), adapter.CatRequest{
				Common:  flags.request(),
				FileID:  args[0],
				Lines:   lines,
				Numbers: numbers,
			})
		},
	}
	cmd.Flags().StringVar(&lines, "lines", "", "line range A,B (1-based, inclusive); default: the whole file")
	cmd.Flags().BoolVarP(&numbers, "line-numbers", "n", false, "prefix every line with its number")
	return cmd
}

func newDoctor(a *adapter.Adapter, flags *common) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check rg, the Kotlin/Native distribution, the klib tool and the cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Doctor(cmd.Context(), adapter.DoctorRequest{Common: flags.request()})
		},
	}
}
