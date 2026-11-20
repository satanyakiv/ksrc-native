package search

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// The extra rg arguments come from the command line after "--" or from the
// rg_args input of the MCP search tool. A model fills the second, and text in
// a hostile repository can steer it, so the arguments are untrusted: rg has
// flags that run a program (--pre), read patterns from a file (-f), follow
// links out of the searched directories (-L) or change the output so that a
// positional path is printed as is. Only the flags below pass.

// extraFlag is one rg flag a caller may add to a search.
type extraFlag struct {
	long  string // name without the dashes
	short byte   // 0 when the flag has no short form
	kind  int
}

const (
	flagBool   = iota // takes no value
	flagString        // takes a value that is passed on as is
	flagNumber        // takes a non-negative integer
)

var extraFlags = []extraFlag{
	{"ignore-case", 'i', flagBool},
	{"case-sensitive", 's', flagBool},
	{"smart-case", 'S', flagBool},
	{"fixed-strings", 'F', flagBool},
	{"word-regexp", 'w', flagBool},
	{"line-regexp", 'x', flagBool},
	{"invert-match", 'v', flagBool},
	{"multiline", 'U', flagBool},
	{"pcre2", 'P', flagBool},
	{"multiline-dotall", 0, flagBool},
	{"glob", 'g', flagString},
	{"iglob", 0, flagString},
	{"max-count", 'm', flagNumber},
	{"after-context", 'A', flagNumber},
	{"before-context", 'B', flagNumber},
	{"max-columns", 0, flagNumber},
}

// AllowedExtraArgs describes the allow-list for hints and documentation.
const AllowedExtraArgs = "-i -s -S -F -w -x -v -U -P --multiline-dotall, -g/--glob <glob>, --iglob <glob>, " +
	"-m/--max-count <n>, -A/--after-context <n>, -B/--before-context <n>, --max-columns <n>"

const extraArgsHint = "these rg arguments are allowed: " + AllowedExtraArgs +
	"; write a value as `--flag value`, `--flag=value` or `-g value`"

func longFlag(name string) (extraFlag, bool) {
	for _, f := range extraFlags {
		if f.long == name {
			return f, true
		}
	}
	return extraFlag{}, false
}

func shortFlag(letter byte) (extraFlag, bool) {
	for _, f := range extraFlags {
		if f.short != 0 && f.short == letter {
			return f, true
		}
	}
	return extraFlag{}, false
}

func badExtraArg(format string, args ...any) error {
	return errs.New(errs.BadArgs, fmt.Sprintf(format, args...), extraArgsHint)
}

// NormalizeExtraArgs checks extra rg arguments against the allow-list and
// rewrites them into one canonical form: "--flag" for a flag without a value
// and "--flag=value" for a flag with one. In that form no value can be read
// by rg as a flag of its own, and the result passes NormalizeExtraArgs again
// unchanged. Anything outside the allow-list is an E_BAD_ARGS error.
func NormalizeExtraArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return nil, badExtraArg("the rg arguments hold a second %q; nothing may follow the flags, and a path cannot be searched", arg)
		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg[2:], "=")
			if name == "context" {
				return nil, badExtraArg("%q is not an rg argument here; use --context of ksrc-native, before the `--`", arg)
			}
			flag, ok := longFlag(name)
			if !ok {
				return nil, badExtraArg("the rg argument %q is not allowed", arg)
			}
			if flag.kind == flagBool {
				if hasValue {
					return nil, badExtraArg("the rg argument %q is not allowed: --%s takes no value", arg, flag.long)
				}
				out = append(out, "--"+flag.long)
				continue
			}
			if !hasValue {
				if i+1 >= len(args) {
					return nil, badExtraArg("the rg argument --%s needs a value", flag.long)
				}
				i++
				value = args[i]
			}
			normalized, err := withValue(flag, "--"+flag.long, value)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		case len(arg) == 2 && arg[0] == '-' && arg[1] != '-':
			if arg[1] == 'C' {
				return nil, badExtraArg("%q is not an rg argument here; use --context of ksrc-native, before the `--`", arg)
			}
			flag, ok := shortFlag(arg[1])
			if !ok {
				return nil, badExtraArg("the rg argument %q is not allowed", arg)
			}
			if flag.kind == flagBool {
				out = append(out, "--"+flag.long)
				continue
			}
			if i+1 >= len(args) {
				return nil, badExtraArg("the rg argument %s needs a value", arg)
			}
			i++
			normalized, err := withValue(flag, arg, args[i])
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		case len(arg) > 2 && arg[0] == '-':
			// A cluster such as -iF. Only flags without a value may be joined,
			// so "-m1" and "-ig x" have one reading: not allowed.
			for j := 1; j < len(arg); j++ {
				flag, ok := shortFlag(arg[j])
				if !ok || flag.kind != flagBool {
					return nil, badExtraArg("the rg argument %q is not allowed: only flags without a value can be joined", arg)
				}
				out = append(out, "--"+flag.long)
			}
		default:
			return nil, badExtraArg("%q is not an rg flag; the searched files are set by --module, and a path cannot be added", arg)
		}
	}
	return out, nil
}

// withValue renders "--flag=value" after checking the value. spelled is the
// flag as the caller wrote it, for the message.
func withValue(flag extraFlag, spelled, value string) (string, error) {
	if value == "" {
		return "", badExtraArg("the rg argument %s needs a value", spelled)
	}
	if flag.kind == flagNumber {
		// ParseUint also refuses a sign, a fraction and a number too large to
		// be a line or column count.
		if _, err := strconv.ParseUint(value, 10, 31); err != nil {
			return "", badExtraArg("the rg argument %s takes a non-negative integer, not %q", spelled, value)
		}
	}
	return "--" + flag.long + "=" + value, nil
}
