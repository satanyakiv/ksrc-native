package search

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// scripted returns a Runner whose rg prints the given stdout and exits with
// the given error.
func scripted(stdout string, exit error) *testutil.Runner {
	return &testutil.Runner{
		Paths: map[string]string{"rg": "/fake/bin/rg"},
		RunFunc: func(_ context.Context, cmd executil.Command) error {
			if _, err := io.WriteString(cmd.Stdout, stdout); err != nil {
				return err
			}
			return exit
		},
	}
}

func collect(lines *[]Line) func(Line) error {
	return func(l Line) error {
		*lines = append(*lines, l)
		return nil
	}
}

var roots = []Root{
	{Dir: "/cache/aaa/src", Prefix: "kn.platform:UIKit:2.4.21!/"},
	{Dir: "/cache/bbb/src", Prefix: "kn.platform:Foundation:2.4.21!/"},
}

const rgStream = `{"type":"begin","data":{"path":{"text":"/cache/aaa/src/ios_arm64/UIApplication.kt"}}}
{"type":"context","data":{"path":{"text":"/cache/aaa/src/ios_arm64/UIApplication.kt"},"lines":{"text":"@kotlinx/cinterop/ExternalObjCClass\n"},"line_number":1,"absolute_offset":0,"submatches":[]}}
{"type":"match","data":{"path":{"text":"/cache/aaa/src/ios_arm64/UIApplication.kt"},"lines":{"text":"public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {\n"},"line_number":2,"absolute_offset":36,"submatches":[{"match":{"text":"class platform/UIKit/UIApplication"},"start":12,"end":46}]}}
{"type":"end","data":{"path":{"text":"/cache/aaa/src/ios_arm64/UIApplication.kt"},"binary_offset":null,"stats":{}}}
{"type":"begin","data":{"path":{"text":"/cache/bbb/src/ios_arm64/_toplevel.kt"}}}
{"type":"match","data":{"path":{"text":"/cache/bbb/src/ios_arm64/_toplevel.kt"},"lines":{"bytes":"dmFsIHg6IEludA0K"},"line_number":7,"absolute_offset":90,"submatches":[{"match":{"text":"x"},"start":4,"end":5}]}}
{"type":"end","data":{"path":{"text":"/cache/bbb/src/ios_arm64/_toplevel.kt"},"binary_offset":null,"stats":{}}}
{"data":{"elapsed_total":{"human":"0.001s","nanos":1,"secs":0},"stats":{}},"type":"summary"}
`

func TestRunMapsRipgrepJSONToFileIDs(t *testing.T) {
	runner := scripted(rgStream, nil)
	var got []Line
	matches, err := Run(context.Background(), runner, Options{Pattern: "class platform/UIKit/UIApplication", Roots: roots, Context: 1}, collect(&got))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if matches != 2 {
		t.Errorf("matches = %d, want 2", matches)
	}

	want := []string{
		"kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 1-@kotlinx/cinterop/ExternalObjCClass",
		"kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 2:13:public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {",
		// The line came as base64 bytes with a CRLF terminator.
		"kn.platform:Foundation:2.4.21!/ios_arm64/_toplevel.kt 7:5:val x: Int",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i, l := range got {
		if Format(l) != want[i] {
			t.Errorf("line %d = %q, want %q", i, Format(l), want[i])
		}
	}

	calls := runner.Calls()
	if len(calls) != 1 {
		t.Fatalf("rg ran %d times, want 1", len(calls))
	}
	if calls[0].Path != "/fake/bin/rg" {
		t.Errorf("rg path = %q", calls[0].Path)
	}
	wantArgs := "--json --no-config --no-ignore --sort path --context 1 --regexp class platform/UIKit/UIApplication -- /cache/aaa/src /cache/bbb/src"
	if gotArgs := strings.Join(calls[0].Args, " "); gotArgs != wantArgs {
		t.Errorf("rg args = %q\nwant      %q", gotArgs, wantArgs)
	}
}

func TestRgArgsPlaceExtraArgumentsBeforeThePattern(t *testing.T) {
	got := rgArgs(Options{Pattern: "-weird", Roots: roots[:1], ExtraArgs: []string{"-i", "-F"}})
	want := "--json --no-config --no-ignore --sort path -i -F --regexp -weird -- /cache/aaa/src"
	if strings.Join(got, " ") != want {
		t.Errorf("rgArgs() = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestRunNoMatchesIsNotAnError(t *testing.T) {
	summary := `{"data":{"elapsed_total":{"human":"0.001s","nanos":1,"secs":0},"stats":{}},"type":"summary"}` + "\n"
	runner := scripted(summary, &executil.ExitError{Path: "rg", Code: 1})
	var got []Line
	matches, err := Run(context.Background(), runner, Options{Pattern: "nothing", Roots: roots}, collect(&got))
	if err != nil || matches != 0 || len(got) != 0 {
		t.Errorf("Run() = %d, %v with %d lines; want 0, nil, 0", matches, err, len(got))
	}
}

func TestRunErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("empty pattern", func(t *testing.T) {
		_, err := Run(ctx, scripted("", nil), Options{Roots: roots}, collect(new([]Line)))
		if errs.Code(err) != errs.BadArgs {
			t.Errorf("error = %v, want %s", err, errs.BadArgs)
		}
	})

	t.Run("rg is not installed", func(t *testing.T) {
		_, err := Run(ctx, &testutil.Runner{}, Options{Pattern: "x", Roots: roots}, collect(new([]Line)))
		if errs.Code(err) != errs.NoRipgrep || !strings.Contains(err.Error(), "brew install ripgrep") {
			t.Errorf("error = %v, want %s with an install hint", err, errs.NoRipgrep)
		}
	})

	t.Run("rg rejects the pattern", func(t *testing.T) {
		runner := scripted("", &executil.ExitError{Path: "rg", Code: 2, Stderr: "rg: regex parse error:\n    (\n    ^\nerror: unclosed group\n"})
		_, err := Run(ctx, runner, Options{Pattern: "(", Roots: roots}, collect(new([]Line)))
		if errs.Code(err) != errs.RgFailed || !strings.Contains(err.Error(), "unclosed group") || !strings.Contains(err.Error(), "-- -F") {
			t.Errorf("error = %v, want %s with rg's message and the -F hint", err, errs.RgFailed)
		}
	})

	t.Run("an extra argument switched JSON off", func(t *testing.T) {
		_, err := Run(ctx, scripted("/cache/aaa/src/ios_arm64/UIApplication.kt\n", nil), Options{Pattern: "x", Roots: roots, ExtraArgs: []string{"-l"}}, collect(new([]Line)))
		if errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), "not JSON") {
			t.Errorf("error = %v, want %s", err, errs.BadArgs)
		}
	})

	t.Run("the consumer stops", func(t *testing.T) {
		stop := errors.New("stdout closed")
		_, err := Run(ctx, scripted(rgStream, nil), Options{Pattern: "x", Roots: roots}, func(Line) error { return stop })
		if !errors.Is(err, stop) {
			t.Errorf("error = %v, want the consumer's error", err)
		}
	})

	t.Run("no roots means no run", func(t *testing.T) {
		runner := scripted(rgStream, nil)
		matches, err := Run(ctx, runner, Options{Pattern: "x"}, collect(new([]Line)))
		if err != nil || matches != 0 || len(runner.Calls()) != 0 {
			t.Errorf("Run() = %d, %v with %d rg runs; want 0, nil, 0", matches, err, len(runner.Calls()))
		}
	})
}

func TestMultiLineMatchYieldsOneLinePerSourceLine(t *testing.T) {
	stream := `{"type":"match","data":{"path":{"text":"/cache/aaa/src/ios_arm64/A.kt"},"lines":{"text":"class A {\n  fun b()\n"},"line_number":4,"absolute_offset":0,"submatches":[{"match":{"text":"A {\n  fun"},"start":6,"end":15}]}}` + "\n"
	var got []Line
	matches, err := Run(context.Background(), scripted(stream, nil), Options{Pattern: "x", Roots: roots}, collect(&got))
	if err != nil {
		t.Fatal(err)
	}
	if matches != 2 || len(got) != 2 {
		t.Fatalf("matches = %d with %d lines, want 2 and 2", matches, len(got))
	}
	if Format(got[0]) != "kn.platform:UIKit:2.4.21!/ios_arm64/A.kt 4:7:class A {" || Format(got[1]) != "kn.platform:UIKit:2.4.21!/ios_arm64/A.kt 5:1:  fun b()" {
		t.Errorf("lines = %q and %q", Format(got[0]), Format(got[1]))
	}
}

func TestFileIDForPathOutsideTheRoots(t *testing.T) {
	if got := fileIDFor("/elsewhere/A.kt", roots); got != "/elsewhere/A.kt" {
		t.Errorf("fileIDFor() = %q, want the path unchanged", got)
	}
	// A sibling directory that shares the root's name as a prefix is outside.
	if got := fileIDFor("/cache/aaa/src-other/A.kt", roots); got != "/cache/aaa/src-other/A.kt" {
		t.Errorf("fileIDFor() = %q, want the path unchanged", got)
	}
}

// TestRunWithRealRipgrep checks the assumptions about rg's JSON on the
// installed binary.
func TestRunWithRealRipgrep(t *testing.T) {
	testutil.RequireRipgrep(t)

	dir := t.TempDir()
	// A .gitignore above the files must not hide them.
	testutil.WriteFile(t, filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "src")
	testutil.WriteFile(t, filepath.Join(root, "ios_arm64", "UIView.kt"), []byte("@Marker\npublic open class platform/UIKit/UIView {\n\n  public open fun layoutSubviews(): kotlin/Unit\n}\n"), 0o644)
	testutil.WriteFile(t, filepath.Join(root, "ios_arm64", "UILabel.kt"), []byte("public open class platform/UIKit/UILabel : platform/UIKit/UIView {\n}\n"), 0o644)

	opts := Options{Pattern: `class platform/UIKit/UIView\b`, Roots: []Root{{Dir: root, Prefix: "kn.platform:UIKit:2.4.21!/"}}}
	var got []string
	emit := func(l Line) error {
		got = append(got, Format(l))
		return nil
	}

	matches, err := Run(context.Background(), executil.OS{}, opts, emit)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 2:13:public open class platform/UIKit/UIView {"}
	if matches != 1 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("matches = %d, lines = %q; want %q", matches, got, want)
	}

	// Context lines, in path order across files.
	got = nil
	opts.Pattern, opts.Context = "UIView", 1
	if _, err := Run(context.Background(), executil.OS{}, opts, emit); err != nil {
		t.Fatalf("Run() with context error = %v", err)
	}
	want = []string{
		"kn.platform:UIKit:2.4.21!/ios_arm64/UILabel.kt 1:59:public open class platform/UIKit/UILabel : platform/UIKit/UIView {",
		"kn.platform:UIKit:2.4.21!/ios_arm64/UILabel.kt 2-}",
		"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 1-@Marker",
		"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 2:34:public open class platform/UIKit/UIView {",
		"kn.platform:UIKit:2.4.21!/ios_arm64/UIView.kt 3-",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines with context =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// No match and a bad pattern.
	opts.Pattern, opts.Context = "NoSuchSymbol", 0
	if matches, err := Run(context.Background(), executil.OS{}, opts, emit); err != nil || matches != 0 {
		t.Errorf("no-match Run() = %d, %v; want 0, nil", matches, err)
	}
	opts.Pattern = "("
	if _, err := Run(context.Background(), executil.OS{}, opts, emit); errs.Code(err) != errs.RgFailed {
		t.Errorf("bad-pattern Run() error = %v, want %s", err, errs.RgFailed)
	}
	// Extra arguments reach rg: a fixed-string search makes "(" legal.
	opts.ExtraArgs = []string{"-F"}
	if matches, err := Run(context.Background(), executil.OS{}, opts, emit); err != nil || matches != 1 {
		t.Errorf("fixed-string Run() = %d, %v; want 1, nil", matches, err)
	}
}
