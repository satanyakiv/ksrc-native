package adapter

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// The tests in this file treat the rg arguments, the target and the SDK as
// values that a model filled in after reading a hostile repository.

// trap is a script that leaves a marker when something runs it, and a file
// outside every module with a line worth stealing.
type trap struct {
	script  string
	marker  string
	outside string
}

func newTrap(t *testing.T) trap {
	t.Helper()
	dir := t.TempDir()
	tr := trap{
		script:  filepath.Join(dir, "pre.sh"),
		marker:  filepath.Join(dir, "marker"),
		outside: filepath.Join(dir, "outside.txt"),
	}
	testutil.WriteFile(t, tr.script, []byte("#!/bin/sh\ntouch '"+tr.marker+"'\ncat\n"), 0o755)
	testutil.WriteFile(t, tr.outside, []byte("hello-from-outside\n"), 0o644)
	return tr
}

func (tr trap) sprung() bool {
	_, err := os.Stat(tr.marker)
	return err == nil
}

func TestSearchRejectsRgArgsOutsideTheAllowList(t *testing.T) {
	w := newWorld(t)
	tr := newTrap(t)
	tests := []struct {
		name    string
		pattern string
		rgArgs  []string
		part    string
	}{
		{"a preprocessor", "presentViewController", []string{"--pre", tr.script}, `"--pre"`},
		{"a preprocessor with =", "presentViewController", []string{"--pre=" + tr.script}, "--pre="},
		{"a path outside the modules", "hello", []string{tr.outside}, "a path cannot be added"},
		{"patterns from a file", "x", []string{"-f", tr.outside}, `"-f"`},
		{"files with matches", "x", []string{"-l"}, `"-l"`},
		{"list files", "x", []string{"--files"}, `"--files"`},
		{"follow links", "x", []string{"-L"}, `"-L"`},
		{"a second --", "hello", []string{"--", tr.outside}, `"--"`},
		{"glob without a value", "x", []string{"-g"}, "-g needs a value"},
		{"max-count that is not a number", "x", []string{"--max-count", "all"}, `"all"`},
	}
	for _, module := range []string{"sdk.apple:UIKit", unModule} {
		for _, tt := range tests {
			t.Run(module+"/"+tt.name, func(t *testing.T) {
				w.reset()
				err := w.a.Search(context.Background(), SearchRequest{Pattern: tt.pattern, Modules: []string{module}, RgArgs: tt.rgArgs})
				if errs.Code(err) != errs.BadArgs {
					t.Fatalf("Search(%q, rg args %q) error = %v, want %s", tt.pattern, tt.rgArgs, err, errs.BadArgs)
				}
				if !strings.Contains(err.Error(), tt.part) || !strings.Contains(err.Error(), "Try: these rg arguments are allowed: -i ") {
					t.Errorf("error %q does not contain %q and the list of allowed arguments", err.Error(), tt.part)
				}
				if w.stdout.Len() != 0 {
					t.Errorf("a refused search wrote %q to stdout", w.stdout.String())
				}
			})
		}
	}
	if tr.sprung() {
		t.Error("a refused search ran the preprocessor script")
	}
	// The check comes before the expensive part.
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a refused search built %d dumps, want 0", len(calls))
	}
}

func TestSearchAcceptsTheAllowedRgArgs(t *testing.T) {
	w := newWorld(t)
	const delegate = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNUserNotificationCenterDelegateProtocol.kt "
	tests := []struct {
		name    string
		req     SearchRequest
		lines   int
		prefix  string // of the first line
		content string // somewhere in the output
	}{
		{"ignore case", SearchRequest{Pattern: "VAL UNERRORDOMAIN", RgArgs: []string{"-i"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:", "UNErrorDomain"},
		{"fixed string", SearchRequest{Pattern: "ObjCClassOf<platform/UserNotifications/UNNotification> {", RgArgs: []string{"-F"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt ", "companion object"},
		{"glob", SearchRequest{Pattern: "^public ", RgArgs: []string{"-g", "*Delegate*", "--max-count", "1"}}, 2, delegate, "UNUserNotificationCenterDelegateProtocol"},
		{"PCRE2", SearchRequest{Pattern: `val UNErrorDomain(?=: kotlin/String)`, RgArgs: []string{"-P"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:", "UNErrorDomain"},
		{"max count", SearchRequest{Pattern: "platform/UserNotifications", RgArgs: []string{"--max-count", "1", "-g", "UNNotification.kt"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt ", ""},
		{"context and a glob", SearchRequest{Pattern: "class platform/UserNotifications/UNNotification ", Context: 1, RgArgs: []string{"-g", "UNNotification.kt"}}, 3, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt 1-", "2:13:public open class"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			tt.req.Modules = []string{unModule}
			if err := w.a.Search(context.Background(), tt.req); err != nil {
				if tt.name == "PCRE2" && strings.Contains(err.Error(), "PCRE2") {
					t.Skip("the installed rg is built without PCRE2")
				}
				t.Fatalf("Search(%q, rg args %q) error = %v", tt.req.Pattern, tt.req.RgArgs, err)
			}
			lines := strings.Split(strings.TrimSpace(w.stdout.String()), "\n")
			if len(lines) != tt.lines || !strings.HasPrefix(lines[0], tt.prefix) || !strings.Contains(w.stdout.String(), tt.content) {
				t.Errorf("stdout =\n%swant %d lines, the first with prefix %q, and %q somewhere", w.stdout.String(), tt.lines, tt.prefix, tt.content)
			}
		})
	}
}

// A pattern is never an argument of rg, whatever it looks like.
func TestSearchTakesAFlagLikePatternAsAPattern(t *testing.T) {
	w := newWorld(t)
	tr := newTrap(t)
	for _, pattern := range []string{"--files", "--pre=" + tr.script, "-l", "--no-json", "--"} {
		w.reset()
		err := w.a.Search(context.Background(), SearchRequest{Pattern: pattern, Modules: []string{"sdk.apple:UIKit"}, RgArgs: []string{"-F"}})
		if err != nil {
			t.Errorf("Search(%q) error = %v, want a plain search without matches", pattern, err)
		}
		if w.stdout.Len() != 0 || !strings.Contains(w.stderr.String(), "no matches for ") {
			t.Errorf("Search(%q): stdout = %q, stderr = %q; want nothing and a no-matches note", pattern, w.stdout.String(), w.stderr.String())
		}
	}
	if tr.sprung() {
		t.Error("a pattern that names a preprocessor made rg run it")
	}
}

// cacheFiles counts the regular files below the cache directory.
func cacheFiles(t *testing.T, w *world) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(w.env[cache.EnvDir], func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// badTargets are values that reach the same directory as a real target on
// some file system, or no directory at all. None is a name in klib/platform.
var badTargets = []string{
	"../platform/ios_arm64",
	"ios_arm64/",
	"./ios_arm64",
	"ios_arm64/../ios_simulator_arm64",
	"IOS_ARM64",
	"Ios_Arm64",
	"ios_arm46",
	" ios_arm64",
}

func TestTargetMustBeAnExactTargetOfTheDistribution(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	operations := map[string]func(Common) error{
		"search a module": func(c Common) error {
			return w.a.Search(ctx, SearchRequest{Common: c, Pattern: "x", Modules: []string{unModule}})
		},
		"search all platform libraries": func(c Common) error {
			return w.a.Search(ctx, SearchRequest{Common: c, Pattern: "x", AllPlatform: true})
		},
		"search without a module": func(c Common) error {
			return w.a.Search(ctx, SearchRequest{Common: c, Pattern: "x"})
		},
		"where": func(c Common) error {
			return w.a.Where(ctx, WhereRequest{Common: c, Coordinate: unModule})
		},
		"deps": func(c Common) error {
			return w.a.Deps(ctx, DepsRequest{Common: c})
		},
	}
	for name, run := range operations {
		for _, target := range badTargets {
			t.Run(name+"/"+target, func(t *testing.T) {
				w.reset()
				err := run(Common{Target: target})
				if errs.Code(err) != errs.NoSources {
					t.Fatalf("%s with --target %q: error = %v, stdout = %q; want %s", name, target, err, w.stdout.String(), errs.NoSources)
				}
				if !strings.Contains(err.Error(), "pass --target with one of: ios_arm64, ios_simulator_arm64") {
					t.Errorf("error %q does not list the targets", err.Error())
				}
				if w.stdout.Len() != 0 {
					t.Errorf("stdout = %q, want nothing", w.stdout.String())
				}
			})
		}
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a refused target ran klib %d times, want 0", len(calls))
	}
	if n := cacheFiles(t, w); n != 0 {
		t.Errorf("a refused target left %d files in the cache, want 0", n)
	}

	// The cache is not poisoned: a normal search still finds the class.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "class platform/UserNotifications/UNNotification ", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt 2:13:") {
		t.Errorf("stdout after the refused targets = %q, want the class line", w.stdout.String())
	}
}

func TestCatChecksTheTargetSegmentOfAFileID(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	before := cacheFiles(t, w)

	for _, fileID := range []string{
		"kn.platform:UserNotifications:2.4.21!/IOS_ARM64/UNNotification.kt",
		"kn.platform:UserNotifications:2.4.21!/ios_arm46/UNNotification.kt",
		"kn.platform:UserNotifications:2.4.21!/../platform/ios_arm64/UNNotification.kt",
		"kn.platform:UserNotifications:2.4.21!/platform/ios_arm64/UNNotification.kt",
	} {
		w.reset()
		err := w.a.Cat(ctx, CatRequest{FileID: fileID})
		if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "pass --target with one of: ios_arm64, ios_simulator_arm64") {
			t.Errorf("Cat(%q) error = %v, want %s with the target list", fileID, err, errs.NoSources)
		}
		if w.stdout.Len() != 0 {
			t.Errorf("Cat(%q) wrote %q to stdout", fileID, w.stdout.String())
		}
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 1 {
		t.Errorf("klib ran %d times, want only the 1 run of the search", len(calls))
	}
	if after := cacheFiles(t, w); after != before {
		t.Errorf("the cache has %d files after the refused file-ids, want the %d from before", after, before)
	}

	// A real target in the file-id wins over --target, as before.
	w.reset()
	err := w.a.Cat(ctx, CatRequest{Common: Common{Target: "ios_simulator_arm64"}, FileID: "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt", Lines: "2"})
	if err != nil || !strings.HasPrefix(w.stdout.String(), "public open class platform/UserNotifications/UNNotification ") {
		t.Errorf("Cat() = %v with stdout %q, want line 2 of the class", err, w.stdout.String())
	}
}

func TestSDKMustBeAnSDKName(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	xcrunRuns := 0
	fakes := w.xcode.Fakes()
	realXcrun := fakes["xcrun"]
	fakes["xcrun"] = func(ctx context.Context, cmd executil.Command) error {
		xcrunRuns++
		return realXcrun(ctx, cmd)
	}
	w.runner.Fakes = fakes

	dir := t.TempDir()
	for _, sdk := range []string{dir, "/tmp", "../sdk", "..", ".", "iphone os", "-iphoneos", "--show-sdk-version", "iphoneos;ls", "26.4"} {
		for name, run := range map[string]func() error{
			"search": func() error {
				return w.a.Search(ctx, SearchRequest{Common: Common{SDK: sdk}, Pattern: "x", Modules: []string{"sdk.apple:UIKit"}})
			},
			"where": func() error {
				return w.a.Where(ctx, WhereRequest{Common: Common{SDK: sdk}, Coordinate: "sdk.apple:UIKit"})
			},
			"cat": func() error {
				return w.a.Cat(ctx, CatRequest{Common: Common{SDK: sdk}, FileID: "sdk.apple:UIKit!/UIView.h"})
			},
			"deps": func() error { return w.a.Deps(ctx, DepsRequest{Common: Common{SDK: sdk}}) },
			"search another group": func() error {
				return w.a.Search(ctx, SearchRequest{Common: Common{SDK: sdk}, Pattern: "x", Modules: []string{unModule}})
			},
		} {
			w.reset()
			err := run()
			if errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), "--sdk") || !strings.Contains(err.Error(), "Try: use iphoneos, iphonesimulator, macosx") {
				t.Errorf("%s with --sdk %q: error = %v, want %s that names --sdk and the usual names", name, sdk, err, errs.BadArgs)
			}
			if w.stdout.Len() != 0 {
				t.Errorf("%s with --sdk %q wrote %q to stdout", name, sdk, w.stdout.String())
			}
		}
	}
	// The SDK part of a coordinate goes to xcrun too.
	for _, module := range []string{"sdk.apple:UIKit:-iphoneos", "sdk.apple:UIKit:..", "sdk.apple:UIKit:iphoneos;ls"} {
		w.reset()
		err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{module}})
		if errs.Code(err) != errs.BadCoord || !strings.Contains(err.Error(), "SDK") {
			t.Errorf("Search(module %q) error = %v, want %s that names the SDK", module, err, errs.BadCoord)
		}
	}
	if xcrunRuns != 0 || len(w.dist.KlibCalls(t)) != 0 {
		t.Errorf("a refused SDK name ran xcrun %d times and klib %d times, want 0 and 0", xcrunRuns, len(w.dist.KlibCalls(t)))
	}

	// Real names still resolve.
	for sdk, want := range map[string]string{"": "iPhoneOS26.4.sdk", "iphoneos": "iPhoneOS26.4.sdk", "iphonesimulator": "iPhoneSimulator26.4.sdk", "iphoneos26.4": "iPhoneOS26.4.sdk"} {
		w.reset()
		if err := w.a.Where(ctx, WhereRequest{Common: Common{SDK: sdk}, Coordinate: "sdk.apple:UIKit"}); err != nil || !strings.Contains(w.stdout.String(), want) {
			t.Errorf("Where(--sdk %q) = %v with stdout %q, want a path with %s", sdk, err, w.stdout.String(), want)
		}
	}
}

func TestDoctorWithABadTargetOrSDK(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	// A target that is not an exact name is a failed check, not "ok".
	for _, target := range []string{"IOS_ARM64", "../platform/ios_arm64", "tvos_arm64"} {
		w.reset()
		err := w.a.Doctor(ctx, DoctorRequest{Common: Common{Target: target}})
		want := "platform: MISSING - no platform libraries for target " + target + "; available targets: ios_arm64, ios_simulator_arm64\n"
		if err == nil || !strings.Contains(w.stdout.String(), want) {
			t.Errorf("Doctor(--target %q) = %v, output:\n%swant the line %q and an error", target, err, w.stdout.String(), want)
		}
	}

	w.reset()
	err := w.a.Doctor(ctx, DoctorRequest{Common: Common{SDK: "/tmp"}})
	if errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), "--sdk") {
		t.Errorf("Doctor(--sdk /tmp) error = %v, want %s that names --sdk", err, errs.BadArgs)
	}
}
