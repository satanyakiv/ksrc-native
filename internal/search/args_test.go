package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func TestNormalizeExtraArgsAcceptsTheAllowList(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"nothing", nil, ""},
		{"short booleans", []string{"-i", "-s", "-S", "-F", "-w", "-x", "-v", "-U", "-P"},
			"--ignore-case --case-sensitive --smart-case --fixed-strings --word-regexp --line-regexp --invert-match --multiline --pcre2"},
		{"long booleans", []string{"--ignore-case", "--case-sensitive", "--smart-case", "--fixed-strings", "--word-regexp", "--line-regexp", "--invert-match", "--multiline", "--pcre2", "--multiline-dotall"},
			"--ignore-case --case-sensitive --smart-case --fixed-strings --word-regexp --line-regexp --invert-match --multiline --pcre2 --multiline-dotall"},
		{"a cluster of short booleans", []string{"-iF", "-Uw"}, "--ignore-case --fixed-strings --multiline --word-regexp"},
		{"short flag with a value", []string{"-g", "*Delegate*", "-m", "1", "-A", "2", "-B", "0"},
			"--glob=*Delegate* --max-count=1 --after-context=2 --before-context=0"},
		{"long flag with a value", []string{"--glob", "UIView.kt", "--iglob", "*.H", "--max-count", "3", "--after-context", "1", "--before-context", "1", "--max-columns", "200"},
			"--glob=UIView.kt --iglob=*.H --max-count=3 --after-context=1 --before-context=1 --max-columns=200"},
		{"long flag with =", []string{"--glob=!*Test*", "--max-count=10", "--max-columns=0"},
			"--glob=!*Test* --max-count=10 --max-columns=0"},
		{"a glob that looks like a flag stays a value", []string{"-g", "--pre", "--glob", "-l"}, "--glob=--pre --glob=-l"},
		{"a glob with = in it", []string{"--glob=a=b"}, "--glob=a=b"},
		{"normalized arguments are accepted again", []string{"--ignore-case", "--glob=*Delegate*"}, "--ignore-case --glob=*Delegate*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeExtraArgs(tt.in)
			if err != nil {
				t.Fatalf("NormalizeExtraArgs(%q) error = %v, want nil", tt.in, err)
			}
			if strings.Join(got, " ") != tt.want {
				t.Errorf("NormalizeExtraArgs(%q) = %q, want %q", tt.in, strings.Join(got, " "), tt.want)
			}
		})
	}
}

func TestNormalizeExtraArgsRejectsEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		part string // must be in the message
	}{
		{"a preprocessor", []string{"--pre", "/tmp/evil.sh"}, `"--pre"`},
		{"a preprocessor with =", []string{"--pre=/tmp/evil.sh"}, `"--pre=/tmp/evil.sh"`},
		{"a preprocessor glob", []string{"--pre-glob", "*"}, `"--pre-glob"`},
		{"a path", []string{"/etc/passwd"}, `"/etc/passwd"`},
		{"a relative path", []string{"-i", "notes.txt"}, `"notes.txt"`},
		{"an empty argument", []string{""}, `""`},
		{"a lone dash", []string{"-"}, `"-"`},
		{"a bare --", []string{"-i", "--", "/etc"}, `"--"`},
		{"files with matches", []string{"-l"}, `"-l"`},
		{"count", []string{"-c"}, `"-c"`},
		{"list files", []string{"--files"}, `"--files"`},
		{"no JSON", []string{"--no-json"}, `"--no-json"`},
		{"vimgrep", []string{"--vimgrep"}, `"--vimgrep"`},
		{"patterns from a file", []string{"-f", "/etc/passwd"}, `"-f"`},
		{"follow links", []string{"-L"}, `"-L"`},
		{"follow links, long", []string{"--follow"}, `"--follow"`},
		{"hidden files", []string{"--hidden"}, `"--hidden"`},
		{"unrestricted", []string{"-u"}, `"-u"`},
		{"no ignore", []string{"--no-ignore"}, `"--no-ignore"`},
		{"search compressed files", []string{"-z"}, `"-z"`},
		{"a config switch", []string{"--no-config"}, `"--no-config"`},
		{"an unknown flag", []string{"--frobnicate"}, `"--frobnicate"`},
		{"a forbidden flag inside a cluster", []string{"-il"}, `"-il"`},
		{"a value flag inside a cluster", []string{"-ig", "x"}, `"-ig"`},
		{"a value attached to a short flag", []string{"-m1"}, `"-m1"`},
		{"a boolean flag with a value", []string{"--ignore-case=yes"}, `"--ignore-case=yes"`},
		{"glob without a value", []string{"-g"}, "-g needs a value"},
		{"long glob without a value", []string{"-i", "--glob"}, "--glob needs a value"},
		{"glob with an empty value", []string{"--glob="}, "--glob needs a value"},
		{"max-count without a value", []string{"--max-count"}, "--max-count needs a value"},
		{"max-count that is not a number", []string{"-m", "many"}, `"many"`},
		{"max-count that is negative", []string{"--max-count=-1"}, `"-1"`},
		{"after-context that is not a number", []string{"-A", "1.5"}, `"1.5"`},
		{"max-columns that is too large", []string{"--max-columns", "99999999999999999999"}, `"99999999999999999999"`},
		{"context belongs to the tool", []string{"-C", "2"}, `"-C"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeExtraArgs(tt.in)
			if errs.Code(err) != errs.BadArgs {
				t.Fatalf("NormalizeExtraArgs(%q) = %q, %v; want %s", tt.in, got, err, errs.BadArgs)
			}
			if !strings.Contains(err.Error(), tt.part) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.part)
			}
			// The hint lists what is allowed.
			for _, allowed := range []string{" Try: ", "-i", "-F", "-g/--glob <glob>", "-m/--max-count <n>", "--max-columns <n>"} {
				if !strings.Contains(err.Error(), allowed) {
					t.Errorf("error %q does not mention %q", err.Error(), allowed)
				}
			}
			if got != nil {
				t.Errorf("NormalizeExtraArgs(%q) returned %q with an error", tt.in, got)
			}
		})
	}
}

func TestContextInExtraArgsPointsAtTheToolFlag(t *testing.T) {
	_, err := NormalizeExtraArgs([]string{"--context", "2"})
	if errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), "--context of ksrc-native") {
		t.Errorf("NormalizeExtraArgs(--context 2) error = %v, want %s that names the tool's own --context", err, errs.BadArgs)
	}
}

// TestRunRejectsExtraArgsBeforeRipgrepRuns pins that Run itself refuses a
// forbidden argument, so no caller can forget the check.
func TestRunRejectsExtraArgsBeforeRipgrepRuns(t *testing.T) {
	for _, extra := range [][]string{{"--pre", "/tmp/evil.sh"}, {"/etc/passwd"}, {"-l"}, {"-g"}} {
		runner := scripted(rgStream, nil)
		var got []Line
		_, err := Run(context.Background(), runner, Options{Pattern: "x", Roots: roots, ExtraArgs: extra}, collect(&got))
		if errs.Code(err) != errs.BadArgs {
			t.Errorf("Run() with %q: error = %v, want %s", extra, err, errs.BadArgs)
		}
		if len(runner.Calls()) != 0 || len(got) != 0 {
			t.Errorf("Run() with %q ran rg %d times and emitted %d lines, want 0 and 0", extra, len(runner.Calls()), len(got))
		}
	}
}

// TestRgArgsKeepThePatternAndExtraArgsApart pins the shape of the command
// line: every argument rg sees is a flag the tool wrote, a "--flag=value"
// pair, or a root after "--".
func TestRgArgsKeepThePatternAndExtraArgsApart(t *testing.T) {
	got, err := rgArgs(Options{Pattern: "--files", Roots: roots[:1], Context: 2, ExtraArgs: []string{"-i", "-g", "--pre"}})
	if err != nil {
		t.Fatalf("rgArgs() error = %v", err)
	}
	want := []string{"--json", "--no-config", "--no-ignore", "--sort", "path", "--context", "2", "--ignore-case", "--glob=--pre", "--regexp=--files", "--", "/cache/aaa/src"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("rgArgs() = %q\nwant       %q", got, want)
	}
}

func TestFileIDForRefusesAPathOutsideTheRoots(t *testing.T) {
	if got, ok := fileIDFor("/cache/aaa/src/ios_arm64/A.kt", roots); !ok || got != "kn.platform:UIKit:2.4.21!/ios_arm64/A.kt" {
		t.Errorf("fileIDFor(inside) = %q, %v; want the file-id and true", got, ok)
	}
	for _, path := range []string{
		"/elsewhere/A.kt",
		// A sibling directory that shares the root's name as a prefix is outside.
		"/cache/aaa/src-other/A.kt",
		"/cache/aaa/src/../../../etc/passwd",
		"/cache/aaa/src",
		"relative/A.kt",
		"",
	} {
		if got, ok := fileIDFor(path, roots); ok || got != "" {
			t.Errorf("fileIDFor(%q) = %q, %v; want \"\" and false", path, got, ok)
		}
	}
}

// A hit outside every root must never be printed, not even as a raw path.
func TestRunFailsOnAHitOutsideTheRoots(t *testing.T) {
	stream := `{"type":"match","data":{"path":{"text":"/etc/outside.txt"},"lines":{"text":"hello-from-outside\n"},"line_number":1,"absolute_offset":0,"submatches":[{"match":{"text":"hello"},"start":0,"end":5}]}}` + "\n"
	var got []Line
	_, err := Run(context.Background(), scripted(stream, nil), Options{Pattern: "hello", Roots: roots}, collect(&got))
	if errs.Code(err) != errs.RgFailed || !strings.Contains(err.Error(), "outside the searched modules") || !strings.Contains(err.Error(), " Try: ") {
		t.Errorf("Run() error = %v, want %s that says the hit is outside the searched modules", err, errs.RgFailed)
	}
	if len(got) != 0 {
		t.Errorf("Run() emitted %+v for a hit outside the roots", got)
	}
}

// realTree is a root with two files for the tests that run the installed rg.
func realTree(t *testing.T) (dir string, opts Options) {
	t.Helper()
	testutil.RequireRipgrep(t)
	dir = t.TempDir()
	root := filepath.Join(dir, "src")
	testutil.WriteFile(t, filepath.Join(root, "ios_arm64", "UIView.kt"), []byte("public open class platform/UIKit/UIView {\n  --files\n  --pre=/bin/sh\n}\n"), 0o644)
	testutil.WriteFile(t, filepath.Join(root, "ios_arm64", "UIViewDelegate.kt"), []byte("public interface platform/UIKit/UIViewDelegate\n"), 0o644)
	return dir, Options{Roots: []Root{{Dir: root, Prefix: "kn.platform:UIKit:2.4.21!/"}}}
}

func runReal(t *testing.T, opts Options) []string {
	t.Helper()
	var got []string
	_, err := Run(context.Background(), executil.OS{}, opts, func(l Line) error {
		got = append(got, Format(l))
		return nil
	})
	if err != nil {
		t.Fatalf("Run(%q, %q) error = %v", opts.Pattern, opts.ExtraArgs, err)
	}
	return got
}

// A pattern that looks like a flag is searched for, not obeyed. Through MCP
// the pattern is a JSON string that no command-line parser has seen.
func TestRealRipgrepTakesAFlagLikePatternAsAPattern(t *testing.T) {
	dir, opts := realTree(t)
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "pre.sh")
	testutil.WriteFile(t, script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0o755)

	tests := []struct {
		pattern string
		extra   []string
		want    []string
	}{
		{"--files", []string{"-F"}, []string{"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 2:3:  --files"}},
		{"--pre=/bin/sh", []string{"-F"}, []string{"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 3:3:  --pre=/bin/sh"}},
		{"--pre=" + script, []string{"-F"}, nil},
		{"-l", nil, nil},
		{"--no-json", nil, nil},
	}
	for _, tt := range tests {
		opts.Pattern, opts.ExtraArgs = tt.pattern, tt.extra
		if got := runReal(t, opts); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
			t.Errorf("pattern %q: lines = %q, want %q", tt.pattern, got, tt.want)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a pattern that names a preprocessor made rg run it")
	}
}

// The user's rg configuration file must not add flags to a search.
func TestRealRipgrepIgnoresTheUserConfiguration(t *testing.T) {
	dir, opts := realTree(t)
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "pre.sh")
	testutil.WriteFile(t, script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0o755)
	config := filepath.Join(dir, "ripgreprc")
	testutil.WriteFile(t, config, []byte("--pre="+script+"\n--invert-match\n"), 0o644)
	t.Setenv("RIPGREP_CONFIG_PATH", config)

	opts.Pattern = "class platform/UIKit/UIView "
	want := "kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 1:13:public open class platform/UIKit/UIView {"
	if got := runReal(t, opts); strings.Join(got, "\n") != want {
		t.Errorf("lines = %q, want %q", got, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("rg ran the preprocessor named in RIPGREP_CONFIG_PATH")
	}
}

// The allowed arguments do what rg documents for them.
func TestRealRipgrepHonoursTheAllowedArguments(t *testing.T) {
	_, opts := realTree(t)
	const view = "kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 1:"
	const delegate = "kn.platform:UIKit:2.4.21!/ios_arm64/UIViewDelegate.kt 1:"
	tests := []struct {
		name    string
		pattern string
		extra   []string
		want    []string // prefixes of the lines
	}{
		{"ignore case", "PLATFORM/UIKIT/UIVIEW ", []string{"-i"}, []string{view}},
		{"fixed string", "UIView {", []string{"-F"}, []string{view}},
		{"glob", "platform/UIKit", []string{"-g", "*Delegate*"}, []string{delegate}},
		{"negated glob", "platform/UIKit", []string{"--glob=!*Delegate*"}, []string{view}},
		{"PCRE2", `platform/UIKit/UIView(?= \{)`, []string{"-P"}, []string{view}},
		{"max count", "UIView", []string{"--max-count", "1"}, []string{view, delegate}},
		{"a cluster", "PLATFORM/UIKIT/UIVIEW {", []string{"-iF"}, []string{view}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "PCRE2" && !hasPCRE2() {
				t.Skip("the installed rg is built without PCRE2")
			}
			opts.Pattern, opts.ExtraArgs = tt.pattern, tt.extra
			got := runReal(t, opts)
			if len(got) != len(tt.want) {
				t.Fatalf("lines = %q, want %d lines", got, len(tt.want))
			}
			for i, prefix := range tt.want {
				if !strings.HasPrefix(got[i], prefix) {
					t.Errorf("line %d = %q, want prefix %q", i, got[i], prefix)
				}
			}
		})
	}
}

func hasPCRE2() bool {
	rg, err := (executil.OS{}).LookPath("rg")
	if err != nil {
		return false
	}
	var out strings.Builder
	return (executil.OS{}).Run(context.Background(), executil.Command{Path: rg, Args: []string{"--pcre2-version"}, Stdout: &out}) == nil
}
