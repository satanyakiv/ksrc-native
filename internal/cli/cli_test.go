package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// harness runs command lines against a fake distribution whose klib tool is
// the script in testdata/fake-klib, and a fake Xcode with two SDKs. Only rg
// is real.
type harness struct {
	t        *testing.T
	a        *adapter.Adapter
	dist     testutil.Distribution
	xcode    *testutil.Xcode
	runner   *testutil.Overlay
	cacheDir string
	out      *bytes.Buffer
	err      *bytes.Buffer
}

const viewControllerHeader = `@interface UIViewController : UIResponder
- (void)presentViewController:(UIViewController *)viewControllerToPresent animated:(BOOL)flag completion:(void (^)(void))completion;
@end
`

func newHarness(t *testing.T) *harness {
	t.Helper()
	testutil.RequireRipgrep(t)

	home := t.TempDir()
	dist := testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.21")
	dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", testutil.ReadFixture(t, testutil.UserNotificationsDump))

	x := testutil.NewXcode(t)
	ios := x.AddSDK(t, "iPhoneOS", "26.4")
	x.AddHeader(t, ios, "UIKit", "UIViewController.h", viewControllerHeader)
	x.AddSwiftOnlyFramework(t, ios, "SwiftUI")
	mac := x.AddSDK(t, "MacOSX", "26.4")
	x.AddVersionedHeader(t, mac, "AppKit", "NSWindow.h", "@interface NSWindow : NSResponder\n@end\n")

	h := &harness{
		t:        t,
		dist:     dist,
		xcode:    x,
		runner:   &testutil.Overlay{Base: executil.OS{}, Fakes: x.Fakes()},
		cacheDir: filepath.Join(t.TempDir(), "cache"),
		out:      &bytes.Buffer{},
		err:      &bytes.Buffer{},
	}
	h.a = &adapter.Adapter{
		Runner: h.runner,
		Stdout: h.out,
		Stderr: h.err,
		Stdin:  strings.NewReader(""),
		Getenv: func(key string) string {
			if key == cache.EnvDir {
				return h.cacheDir
			}
			return ""
		},
		UserHome:   home,
		IsTerminal: func() bool { return false },
	}
	return h
}

// run executes one command line and returns the exit code, stdout and stderr.
func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	h.out.Reset()
	h.err.Reset()
	// --project points at an empty directory, so the repository's own files
	// never influence a test. It goes first, because arguments after "--"
	// belong to rg.
	args = append([]string{"--project", h.t.TempDir()}, args...)
	code := Run(context.Background(), args, h.a)
	return code, h.out.String(), h.err.String()
}

const (
	unModule = "kn.platform:UserNotifications"
	unFileID = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt"
	unHeader = "public open class platform/UserNotifications/UNNotification : platform/darwin/NSObject, platform/Foundation/NSCopyingProtocol, platform/Foundation/NSSecureCodingProtocol {"
)

func TestSearchThenCat(t *testing.T) {
	h := newHarness(t)

	code, stdout, stderr := h.run("search", "class platform/UserNotifications/UNNotification ", "--module", unModule)
	if code != 0 {
		t.Fatalf("search exit code = %d, stderr:\n%s", code, stderr)
	}
	if want := unFileID + " 2:13:" + unHeader + "\n"; stdout != want {
		t.Errorf("search stdout = %q\nwant            %q", stdout, want)
	}

	// The file-id of the search line feeds cat.
	fileID, _, _ := strings.Cut(stdout, " ")
	code, stdout, stderr = h.run("cat", fileID, "--lines", "1,2")
	if code != 0 {
		t.Fatalf("cat exit code = %d, stderr:\n%s", code, stderr)
	}
	if want := "@kotlinx/cinterop/ExternalObjCClass\n" + unHeader + "\n"; stdout != want {
		t.Errorf("cat stdout = %q\nwant         %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("cat of a cached module wrote to stderr: %q", stderr)
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 1 {
		t.Errorf("klib ran %d times for a search and a cat, want 1", len(calls))
	}
}

func TestAliases(t *testing.T) {
	h := newHarness(t)

	code, stdout, stderr := h.run("rg", "val UNErrorDomain", "-m", unModule)
	if code != 0 || !strings.HasPrefix(stdout, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:public final val UNErrorDomain: kotlin/String?") {
		t.Errorf("rg alias: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, stderr = h.run("open", unFileID, "-n", "--lines", "2")
	if code != 0 || stdout != "2\t"+unHeader+"\n" {
		t.Errorf("open alias: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestSearchFlags(t *testing.T) {
	h := newHarness(t)

	// Arguments after "--" reach rg; flags may come before the pattern.
	code, stdout, stderr := h.run("search", "--module", unModule, "-C", "1", "COMPANION OBJECT PLATFORM/USERNOTIFICATIONS/UNNOTIFICATION\\.", "--", "-i")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], ":14:public final companion object platform/UserNotifications/UNNotification.Companion") {
		t.Errorf("stdout =\n%s\nwant a context line, the companion match and a context line", stdout)
	}

	// Two modules in one search; the second is cached by the first run.
	code, stdout, _ = h.run("search", "val UNErrorDomain", "--module", unModule, "--module", unModule+":2.4.21", "--kotlin-version", "2.4.21", "--target", "ios_arm64")
	if code != 0 || strings.Count(stdout, "UNErrorDomain") < 1 {
		t.Errorf("two modules: code = %d, stdout = %q", code, stdout)
	}

	// No match is a success with a note on stderr.
	code, stdout, stderr = h.run("search", "NoSuchSymbolAnywhere", "--module", unModule)
	if code != 0 || stdout != "" || !strings.Contains(stderr, "no matches") {
		t.Errorf("no match: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestDoctorCommand(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor exit code = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, name := range []string{"rg: ok - ", "kotlin-native: ok - 2.4.21 at ", "klib: ok - ", "platform: ok - ", "cache: ok - "} {
		if !strings.Contains(stdout, name) {
			t.Errorf("doctor output lacks %q:\n%s", name, stdout)
		}
	}

	h.a.UserHome = t.TempDir()
	code, stdout, stderr = h.run("doctor")
	if code != 1 || !strings.Contains(stdout, "kotlin-native: MISSING - E_NO_KONAN: ") || !strings.HasPrefix(stderr, "E_DOCTOR: ") {
		t.Errorf("doctor without a distribution: code = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestErrorsGoToStderrWithACodeAndAHint(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		args   []string
		prefix string
		part   string
	}{
		{"unknown framework", []string{"search", "x", "--module", "kn.platform:Nope"}, "E_NO_SOURCES: ", `no platform library "Nope"`},
		{"version that is not installed", []string{"search", "x", "--module", unModule, "--kotlin-version", "1.9.0"}, "E_NO_KONAN: ", "installed: 2.4.21"},
		{"nothing cached and no module", []string{"search", "x"}, "E_NO_SOURCES: ", "--module kn.platform:UIKit"},
		{"search without a pattern", []string{"search", "--module", unModule}, "E_BAD_ARGS: ", "search takes one pattern, got 0"},
		{"search with two patterns", []string{"search", "a", "b", "--module", unModule}, "E_BAD_ARGS: ", "got 2 arguments"},
		{"cat without a file-id", []string{"cat"}, "E_BAD_ARGS: ", "cat takes one file-id"},
		{"cat of a path", []string{"cat", "UNNotification.kt"}, "E_BAD_FILE_ID: ", "cannot parse the file-id"},
		{"cat of a missing file", []string{"cat", "kn.platform:UserNotifications:2.4.21!/ios_arm64/Nope.kt"}, "E_NO_FILE: ", "Nope.kt"},
		{"cat with a bad range", []string{"cat", unFileID, "--lines", "x"}, "E_BAD_LINES: ", "cannot parse the line range"},
		{"unknown flag", []string{"search", "x", "--nope"}, "E_BAD_ARGS: ", "unknown flag: --nope"},
		{"unknown command", []string{"frobnicate"}, "E_BAD_ARGS: ", `unknown command "frobnicate"`},
		{"doctor with an argument", []string{"doctor", "extra"}, "E_BAD_ARGS: ", "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tt.args...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			// The error is the last line; a progress line may come before it.
			lines := strings.Split(strings.TrimSpace(stderr), "\n")
			last := lines[len(lines)-1]
			if !strings.HasPrefix(last, tt.prefix) || !strings.Contains(last, tt.part) {
				t.Errorf("last stderr line = %q, want prefix %q and %q", last, tt.prefix, tt.part)
			}
			if !strings.Contains(last, " Try: ") {
				t.Errorf("last stderr line = %q, want a hint", last)
			}
			for _, line := range lines[:len(lines)-1] {
				if !strings.HasPrefix(line, "ksrc-native: ") {
					t.Errorf("unexpected stderr line before the error: %q", line)
				}
			}
		})
	}
}

func TestVersionAndHelp(t *testing.T) {
	h := newHarness(t)

	previous := Version
	Version = "9.8.7"
	defer func() { Version = previous }()

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	h.a.Stdout, h.a.Stderr = out, errOut
	if code := Run(context.Background(), []string{"--version"}, h.a); code != 0 || out.String() != "ksrc-native 9.8.7\n" {
		t.Errorf("--version: code = %d, stdout = %q", code, out.String())
	}

	out.Reset()
	if code := Run(context.Background(), nil, h.a); code != 0 {
		t.Errorf("no arguments: code = %d, want 0", code)
	}
	for _, part := range []string{"search", "cat", "doctor", "--konan-home", "--kotlin-version", "--project", "--target"} {
		if !strings.Contains(out.String(), part) {
			t.Errorf("help lacks %q:\n%s", part, out.String())
		}
	}

	out.Reset()
	if code := Run(context.Background(), []string{"search", "--help"}, h.a); code != 0 || !strings.Contains(out.String(), "<file-id> <line>:<col>:<text>") || !strings.Contains(out.String(), "Aliases:") {
		t.Errorf("search --help: code = %d, output:\n%s", code, out.String())
	}
}

func TestModuleVersion(t *testing.T) {
	for in, want := range map[string]string{
		"":         "dev",
		"(devel)":  "dev",
		"v0.1.0":   "0.1.0",
		"v1.2.3-0": "1.2.3-0",
		"0.2.0":    "0.2.0",
	} {
		if got := moduleVersion(in); got != want {
			t.Errorf("moduleVersion(%q) = %q, want %q", in, got, want)
		}
	}

	// An injected version wins over the build info.
	previous := Version
	Version = "9.8.7"
	defer func() { Version = previous }()
	if got := effectiveVersion(); got != "9.8.7" {
		t.Errorf("effectiveVersion() = %q, want the injected 9.8.7", got)
	}
}

func TestSplitSearchArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		dash    int
		pattern string
		rgArgs  string
		wantErr bool
	}{
		{"pattern only", []string{"p"}, -1, "p", "", false},
		{"pattern and rg arguments", []string{"p", "-i", "-F"}, 1, "p", "-i -F", false},
		{"dash without rg arguments", []string{"p"}, 1, "p", "", false},
		{"no pattern", nil, -1, "", "", true},
		{"only rg arguments", []string{"-i"}, 0, "", "", true},
		{"two patterns", []string{"a", "b"}, -1, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, rgArgs, err := splitSearchArgs(tt.args, tt.dash)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if pattern != tt.pattern || strings.Join(rgArgs, " ") != tt.rgArgs {
				t.Errorf("got %q and %q, want %q and %q", pattern, rgArgs, tt.pattern, tt.rgArgs)
			}
		})
	}
}
