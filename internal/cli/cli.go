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
	"github.com/satanyakiv/ksrc-native/internal/mcpserver"
	"github.com/satanyakiv/ksrc-native/internal/xcode"
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
	sdk           string
}

func (c *common) request() adapter.Common {
	return adapter.Common{
		KonanHome:     c.konanHome,
		KotlinVersion: c.kotlinVersion,
		Project:       c.project,
		Target:        c.target,
		SDK:           c.sdk,
	}
}

func newRoot(a *adapter.Adapter) *cobra.Command {
	flags := &common{}
	root := &cobra.Command{
		Use:   "ksrc-native",
		Short: "Search Kotlin/Native platform APIs, the native stdlib and Apple SDK headers",
		Long: `ksrc-native searches and reads the sources behind Kotlin/Native code that a
Gradle dependency search does not reach:

  kn.platform:<Framework>[:<kotlinVersion>]  Kotlin signatures of platform.UIKit, platform.Foundation, ...
  kn.stdlib:native[:<kotlinVersion>]         sources of the native stdlib and kotlinx.cinterop
  sdk.apple:<Framework>[:<sdk>]              Objective-C headers of an Xcode SDK

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
	pf.StringVar(&flags.sdk, "sdk", xcode.DefaultSDK, "Apple SDK of sdk.apple modules, for example iphoneos, iphonesimulator, macosx")

	root.AddCommand(
		newSearch(a, flags),
		newCat(a, flags),
		newDeps(a, flags),
		newWhere(a, flags),
		newDoctor(a, flags),
		newClean(a),
		newMCP(a, flags),
	)
	return root
}

func newMCP(a *adapter.Adapter, flags *common) *cobra.Command {
	var tools string
	cmd := &cobra.Command{
		Use:   "mcp [--tools=search,cat,deps|all]",
		Short: "Run a Model Context Protocol server on stdin and stdout",
		Long: `Mcp serves the commands as MCP tools over stdio, for agents that talk MCP
instead of running a shell. A tool takes the inputs of the command of the
same name and returns what the command prints.

Tools: search, cat and deps by default; where when named in --tools or with
--tools=all. The global flags given here (--project, --target, --sdk,
--kotlin-version, --konan-home) are the defaults of every tool call; a call
may override them.`,
		Example: `  ksrc-native mcp
  ksrc-native mcp --tools=all --project /path/to/project
  claude mcp add ksrc-native -- ksrc-native mcp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			names, err := mcpserver.ParseTools(tools)
			if err != nil {
				return err
			}
			if a.Stdin == nil {
				return usageError("mcp needs stdin to read requests from")
			}
			return mcpserver.Run(cmd.Context(), a, mcpserver.Options{
				Version:  Version,
				Tools:    names,
				Defaults: flags.request(),
			}, mcpserver.StdioTransport(a.Stdin, a.Stdout))
		},
	}
	cmd.Flags().StringVar(&tools, "tools", "", "comma-separated tools to serve, or `all` (default: search,cat,deps)")
	return cmd
}

func newSearch(a *adapter.Adapter, flags *common) *cobra.Command {
	var (
		modules      []string
		allPlatform  bool
		contextLines int
	)
	cmd := &cobra.Command{
		Use:     "search <pattern> [--module <coordinate>]... [-- <rg-args>]",
		Aliases: []string{"rg"},
		Short:   "Search modules with a regular expression",
		Long: `Search runs ripgrep over the named modules and prints one line per match:

  <file-id> <line>:<col>:<text>

Modules:
  kn.platform:<Framework>[:<kotlinVersion>]  the first search of a framework builds its dump (a few seconds)
  kn.stdlib:native[:<kotlinVersion>]         extracted on first use
  sdk.apple:<Framework>[:<sdk>]              searched in place

Without --module, the platform libraries that are already cached and the
stdlib are searched. --all-platform builds and searches every platform
library of the target; that takes minutes the first time.

Names in a platform dump are fully qualified with slashes:
platform/UIKit/UIView, kotlin/String. Arguments after "--" go to rg, for
example "-- -i" or "-- -F".`,
		Example: `  ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
  ksrc-native search 'fun dataTaskWithURL' --module kn.platform:Foundation --context 2
  ksrc-native search 'fun <T> memScoped' --module kn.stdlib:native
  ksrc-native search 'presentViewController' --module sdk.apple:UIKit -- -F`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern, rgArgs, err := splitSearchArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			return a.Search(cmd.Context(), adapter.SearchRequest{
				Common:      flags.request(),
				Pattern:     pattern,
				Modules:     modules,
				AllPlatform: allPlatform,
				Context:     contextLines,
				RgArgs:      rgArgs,
			})
		},
	}
	cmd.Flags().StringArrayVarP(&modules, "module", "m", nil, "module to search; repeatable")
	cmd.Flags().BoolVar(&allPlatform, "all-platform", false, "build and search every platform library of the target")
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

// oneArg returns an argument validator for commands that take exactly one
// positional argument.
func oneArg(command, what, example string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errs.New(errs.BadArgs,
				fmt.Sprintf("%s takes one %s, got %d arguments", command, what, len(args)),
				"for example: "+example)
		}
		return nil
	}
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
  ksrc-native cat 'kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt' -n --lines 100,120
  ksrc-native cat 'sdk.apple:UIKit:iphoneos26.4!/UIViewController.h' --lines 1,60`,
		Args: oneArg("cat", "file-id", "ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' (quote it, it contains `!`)"),
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

func newDeps(a *adapter.Adapter, flags *common) *cobra.Command {
	return &cobra.Command{
		Use:   "deps",
		Short: "List the modules that can be searched",
		Long: `Deps prints one module per line: the platform libraries of --target with the
state of their cached dump, the stdlib sources, and the installed Apple SDKs.
An sdk.apple line stands for all frameworks of one SDK.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Deps(cmd.Context(), adapter.DepsRequest{Common: flags.request()})
		},
	}
}

func newWhere(a *adapter.Adapter, flags *common) *cobra.Command {
	return &cobra.Command{
		Use:   "where <coordinate>",
		Short: "Print the path a module is read from",
		Long: `Where prints the klib directory of a platform library, the stdlib sources zip
or the Headers directory of an SDK framework. It builds nothing.`,
		Example: `  ksrc-native where kn.platform:UIKit
  ksrc-native where kn.stdlib:native
  ksrc-native where sdk.apple:UIKit --sdk iphonesimulator`,
		Args: oneArg("where", "coordinate", "ksrc-native where kn.platform:UIKit"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Where(cmd.Context(), adapter.WhereRequest{Common: flags.request(), Coordinate: args[0]})
		},
	}
}

func newDoctor(a *adapter.Adapter, flags *common) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check rg, the Kotlin/Native distribution, Xcode and the cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Doctor(cmd.Context(), adapter.DoctorRequest{Common: flags.request()})
		},
	}
}

func newClean(a *adapter.Adapter) *cobra.Command {
	var yes, dryRun bool
	cmd := &cobra.Command{
		Use:   "clean [-y] [--dry-run]",
		Short: "Show the cache and delete it",
		Long: `Clean lists the cached platform dumps and stdlib sources with their sizes and
deletes them. It asks before deleting when run in a terminal; -y skips the
question. Without a terminal and without -y nothing is deleted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Clean(cmd.Context(), adapter.CleanRequest{Yes: yes, DryRun: dryRun})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted and delete nothing")
	return cmd
}
