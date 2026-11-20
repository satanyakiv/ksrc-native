package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/provider"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

const (
	stdlibModule = "kn.stdlib:native"
	utilsFileID  = "kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt"
	vcFileID     = "sdk.apple:UIKit:iphoneos26.4!/UIViewController.h"
)

func TestSearchAndCatStdlib(t *testing.T) {
	w := newWorld(t)
	w.dist.AddStdlibSources(t, nil)
	ctx := context.Background()

	if err := w.a.Search(ctx, SearchRequest{Pattern: `fun <R> memScoped`, Modules: []string{stdlibModule}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := utilsFileID + " 5:15:public inline fun <R> memScoped(block: MemScope.() -> R): R {\n"
	if w.stdout.String() != want {
		t.Errorf("stdout = %q\nwant     %q", w.stdout.String(), want)
	}
	if !strings.Contains(w.stderr.String(), "extracting kn.stdlib:native:2.4.21") {
		t.Errorf("stderr = %q, want the progress line of the first extraction", w.stderr.String())
	}

	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: utilsFileID, Lines: "1,3"}); err != nil {
		t.Fatalf("Cat() error = %v", err)
	}
	if got, want := w.stdout.String(), "package kotlinx.cinterop\n\npublic class MemScope\n"; got != want {
		t.Errorf("cat stdout = %q, want %q", got, want)
	}
	if w.stderr.Len() != 0 {
		t.Errorf("a cached cat wrote to stderr: %q", w.stderr.String())
	}
	// No klib run is needed for the stdlib.
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("klib ran %d times for a stdlib search", len(calls))
	}
}

func TestSearchAndCatAppleHeaders(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.a.Search(ctx, SearchRequest{Pattern: "presentViewController", Modules: []string{"sdk.apple:UIKit"}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := vcFileID + " 2:9:- (void)presentViewController:(UIViewController *)viewControllerToPresent animated:(BOOL)flag completion:(void (^)(void))completion;\n"
	if w.stdout.String() != want {
		t.Errorf("stdout = %q\nwant     %q", w.stdout.String(), want)
	}
	// Headers are searched in place: nothing is cached and nothing is logged.
	if w.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", w.stderr.String())
	}
	if _, err := os.Stat(w.env[cache.EnvDir]); !os.IsNotExist(err) {
		t.Errorf("a header search created the cache directory (%v)", err)
	}

	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: vcFileID, Lines: "1"}); err != nil {
		t.Fatalf("Cat() error = %v", err)
	}
	if got, want := w.stdout.String(), "@interface UIViewController : UIResponder\n"; got != want {
		t.Errorf("cat stdout = %q, want %q", got, want)
	}

	// --sdk selects another SDK; the SDK in a coordinate wins over --sdk.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{SDK: "iphonesimulator"}, Pattern: "simulator SDK", Modules: []string{"sdk.apple:UIKit"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "sdk.apple:UIKit:iphonesimulator26.4!/UIViewController.h 1:4:") {
		t.Errorf("stdout = %q, want a simulator file-id", w.stdout.String())
	}
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{SDK: "iphonesimulator"}, Pattern: "@interface", Modules: []string{"sdk.apple:UIKit:iphoneos26.4"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(w.stdout.String(), "sdk.apple:UIKit:iphoneos26.4!/"); got != 2 {
		t.Errorf("stdout = %q, want two hits in the iphoneos SDK", w.stdout.String())
	}

	// A macOS framework with a linked Headers directory, and a file-id whose
	// SDK part is the short name.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{SDK: "macosx"}, Pattern: "NSWindow", Modules: []string{"sdk.apple:AppKit"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "sdk.apple:AppKit:macosx26.4!/NSWindow.h 1:12:") {
		t.Errorf("stdout = %q, want an AppKit hit", w.stdout.String())
	}
	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: "sdk.apple:AppKit:macosx!/NSWindow.h", Lines: "1"}); err != nil || !strings.HasPrefix(w.stdout.String(), "@interface NSWindow") {
		t.Errorf("Cat(macosx file-id) = %q, %v", w.stdout.String(), err)
	}
}

func TestAppleHeadersNeedNoKotlinNative(t *testing.T) {
	w := newWorld(t)
	w.a.UserHome = t.TempDir() // no distribution anywhere

	if err := w.a.Search(context.Background(), SearchRequest{Pattern: "presentViewController", Modules: []string{"sdk.apple:UIKit"}}); err != nil {
		t.Fatalf("Search() error = %v, want sdk.apple to work without Kotlin/Native", err)
	}
	if !strings.HasPrefix(w.stdout.String(), vcFileID+" ") {
		t.Errorf("stdout = %q", w.stdout.String())
	}
}

func TestAppleErrors(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	tests := []struct {
		name string
		req  SearchRequest
		code string
		part string
	}{
		{"swift-only framework", SearchRequest{Pattern: "x", Modules: []string{"sdk.apple:SwiftUI"}}, errs.NoSources, "no Objective-C headers"},
		{"unknown framework", SearchRequest{Pattern: "x", Modules: []string{"sdk.apple:Nope"}}, errs.NoSources, `no framework "Nope"`},
		{"unknown SDK flag", SearchRequest{Common: Common{SDK: "watchos"}, Pattern: "x", Modules: []string{"sdk.apple:UIKit"}}, errs.NoSources, `the Apple SDK "watchos" is not installed`},
		{"unknown SDK in the coordinate", SearchRequest{Pattern: "x", Modules: []string{"sdk.apple:UIKit:xros9"}}, errs.NoSources, `the Apple SDK "xros9" is not installed`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := w.a.Search(ctx, tt.req)
			if errs.Code(err) != tt.code || !strings.Contains(err.Error(), tt.part) {
				t.Errorf("Search() error = %v, want %s with %q", err, tt.code, tt.part)
			}
		})
	}

	// Xcode is absent: only sdk.apple fails, and with its own code.
	w.runner.Fakes = nil
	w.runner.Hidden = []string{"xcrun", "xcode-select"}
	err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{"sdk.apple:UIKit"}})
	if errs.Code(err) != errs.NoXcode || !strings.HasPrefix(err.Error(), "E_NO_XCODE: ") || !strings.Contains(err.Error(), " Try: ") {
		t.Errorf("Search() without Xcode: error = %v, want E_NO_XCODE with a hint", err)
	}
	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain", Modules: []string{unModule}}); err != nil {
		t.Errorf("a platform search without Xcode failed: %v", err)
	}
}

func TestSearchMixedModules(t *testing.T) {
	w := newWorld(t)
	w.dist.AddStdlibSources(t, nil)

	// The SDK name of an sdk.apple coordinate is not a Kotlin version, so it
	// does not clash with the Kotlin version of the others.
	err := w.a.Search(context.Background(), SearchRequest{
		Pattern: `val UNErrorDomain|memScoped\(|presentViewController`,
		Modules: []string{unModule + ":2.4.21", stdlibModule, "sdk.apple:UIKit:iphoneos26.4", unModule},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	out := w.stdout.String()
	for _, prefix := range []string{
		"kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt ",
		utilsFileID + " ",
		vcFileID + " ",
	} {
		if strings.Count(out, prefix) != 1 {
			t.Errorf("stdout has %d lines for %q, want 1 (a module named twice is searched once):\n%s", strings.Count(out, prefix), prefix, out)
		}
	}

	// kn.* modules must still agree on one Kotlin version.
	err = w.a.Search(context.Background(), SearchRequest{Pattern: "x", Modules: []string{unModule + ":2.4.21", stdlibModule + ":2.4.10"}})
	if errs.Code(err) != errs.BadCoord || !strings.Contains(err.Error(), "two Kotlin versions") {
		t.Errorf("Search() error = %v, want %s for two Kotlin versions", err, errs.BadCoord)
	}
}

func TestSearchWithoutModuleAddsTheStdlib(t *testing.T) {
	w := newWorld(t)
	w.dist.AddStdlibSources(t, nil)
	ctx := context.Background()

	// Nothing is cached: the stdlib alone is searched, and no dump is built.
	if err := w.a.Search(ctx, SearchRequest{Pattern: `memScoped\(|UNErrorDomain`}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := w.stdout.String(); !strings.HasPrefix(got, utilsFileID+" ") || strings.Contains(got, "kn.platform") {
		t.Errorf("stdout = %q, want a stdlib hit only", got)
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a search without --module built %d dumps", len(calls))
	}

	// After one platform search, both are covered.
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: `memScoped\(|val UNErrorDomain`}); err != nil {
		t.Fatal(err)
	}
	out := w.stdout.String()
	if !strings.Contains(out, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt ") || !strings.Contains(out, utilsFileID+" ") {
		t.Errorf("stdout = %q, want hits in the cached dump and in the stdlib", out)
	}

	// The no-matches note names what was searched.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "NoSuchSymbolAnywhere"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.stderr.String(), "in kn.platform:UserNotifications:2.4.21, kn.stdlib:native:2.4.21 (ios_arm64)") {
		t.Errorf("stderr = %q, want both modules named", w.stderr.String())
	}
}

func TestSearchAllPlatform(t *testing.T) {
	w := newWorld(t)
	w.dist.AddStdlibSources(t, nil)
	dump := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	// A second library with the same content, and one without declarations.
	second := strings.ReplaceAll(string(dump), "UserNotifications", "OtherKit")
	w.dist.AddPlatformKlib(t, "ios_arm64", "OtherKit", []byte(second))
	w.dist.AddPlatformKlib(t, "ios_arm64", "Empty", []byte("library {\n  library fragment {\n    // package name: platform.Empty\n\n    package {\n    }\n  }\n}\n"))
	ctx := context.Background()

	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain", AllPlatform: true}); err != nil {
		t.Fatalf("Search(--all-platform) error = %v", err)
	}
	out := w.stdout.String()
	for _, prefix := range []string{"kn.platform:OtherKit:2.4.21!/ios_arm64/_toplevel.kt ", "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt "} {
		if !strings.Contains(out, prefix) {
			t.Errorf("stdout lacks %q:\n%s", prefix, out)
		}
	}
	// Every library of the target was dumped once, the empty one included.
	if calls := w.dist.KlibCalls(t); len(calls) != 3 {
		t.Errorf("klib ran %d times, want 3: %q", len(calls), calls)
	}

	// The second run reads the cache for the two libraries with content.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain", AllPlatform: true}); err != nil {
		t.Fatal(err)
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 4 {
		t.Errorf("klib ran %d times after the second run, want 4 (only the empty library again)", len(calls))
	}

	err := w.a.Search(ctx, SearchRequest{Common: Common{Target: "tvos_arm64"}, Pattern: "x", AllPlatform: true})
	if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), `target "tvos_arm64"`) {
		t.Errorf("Search(--all-platform, unknown target) error = %v, want %s", err, errs.NoSources)
	}
}

func TestWhere(t *testing.T) {
	w := newWorld(t)
	zipPath := w.dist.AddStdlibSources(t, nil)
	ctx := context.Background()

	tests := []struct {
		name string
		req  WhereRequest
		want string
	}{
		{"platform klib", WhereRequest{Coordinate: "kn.platform:usernotifications"},
			filepath.Join(w.dist.Home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.UserNotifications")},
		{"platform klib of another target", WhereRequest{Common: Common{Target: "ios_simulator_arm64"}, Coordinate: unModule},
			filepath.Join(w.dist.Home, "klib", "platform", "ios_simulator_arm64", "org.jetbrains.kotlin.native.platform.UserNotifications")},
		{"platform klib of another version", WhereRequest{Coordinate: unModule + ":2.4.10"},
			filepath.Join(w.old.Home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.UserNotifications")},
		{"stdlib zip", WhereRequest{Coordinate: stdlibModule}, zipPath},
		{"SDK headers", WhereRequest{Coordinate: "sdk.apple:UIKit"},
			filepath.Join(w.xcode.Developer, "Platforms", "iPhoneOS.platform", "Developer", "SDKs", "iPhoneOS26.4.sdk", "System", "Library", "Frameworks", "UIKit.framework", "Headers")},
		{"SDK headers of another SDK", WhereRequest{Common: Common{SDK: "macosx"}, Coordinate: "sdk.apple:AppKit"},
			filepath.Join(w.xcode.Developer, "Platforms", "MacOSX.platform", "Developer", "SDKs", "MacOSX26.4.sdk", "System", "Library", "Frameworks", "AppKit.framework", "Headers")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			if err := w.a.Where(ctx, tt.req); err != nil {
				t.Fatalf("Where() error = %v", err)
			}
			if got := w.stdout.String(); got != tt.want+"\n" {
				t.Errorf("stdout = %q\nwant     %q", got, tt.want+"\n")
			}
		})
	}

	// where builds nothing.
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("where ran klib %d times", len(calls))
	}
	if _, err := os.Stat(w.env[cache.EnvDir]); !os.IsNotExist(err) {
		t.Errorf("where created the cache directory (%v)", err)
	}

	errorTests := []struct {
		name, coord, code string
	}{
		{"unparsable", "UIKit", errs.BadCoord},
		{"unknown group", "com.example:lib", errs.BadCoord},
		{"unknown framework", "kn.platform:Nope", errs.NoSources},
		{"unknown stdlib module", "kn.stdlib:jvm", errs.NoSources},
		{"swift-only framework", "sdk.apple:SwiftUI", errs.NoSources},
		{"version that is not installed", "kn.platform:UserNotifications:1.9.0", errs.NoKonan},
	}
	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			err := w.a.Where(ctx, WhereRequest{Coordinate: tt.coord})
			if errs.Code(err) != tt.code || w.stdout.Len() != 0 {
				t.Errorf("Where(%q) error = %v, stdout %q; want %s and no output", tt.coord, err, w.stdout.String(), tt.code)
			}
		})
	}
}

func TestDeps(t *testing.T) {
	w := newWorld(t)
	zipPath := w.dist.AddStdlibSources(t, nil)
	w.dist.AddPlatformKlib(t, "ios_arm64", "Foundation", nil)
	ctx := context.Background()

	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	w.reset()
	if err := w.a.Deps(ctx, DepsRequest{}); err != nil {
		t.Fatalf("Deps() error = %v", err)
	}
	sdks := filepath.Join(w.xcode.Developer, "Platforms")
	want := strings.Join([]string{
		"kn.platform:Foundation:2.4.21 (ios_arm64, dump cached: no)",
		"kn.platform:UserNotifications:2.4.21 (ios_arm64, dump cached: yes)",
		"kn.stdlib:native:2.4.21 (extracted: no, " + zipPath + ")",
		"sdk.apple:<Framework>:iphoneos26.4 (1 frameworks with headers, " + realPath(t, filepath.Join(sdks, "iPhoneOS.platform", "Developer", "SDKs", "iPhoneOS.sdk")) + ")",
		"sdk.apple:<Framework>:iphonesimulator26.4 (1 frameworks with headers, " + realPath(t, filepath.Join(sdks, "iPhoneSimulator.platform", "Developer", "SDKs", "iPhoneSimulator.sdk")) + ")",
		"sdk.apple:<Framework>:macosx26.4 (1 frameworks with headers, " + realPath(t, filepath.Join(sdks, "MacOSX.platform", "Developer", "SDKs", "MacOSX.sdk")) + ")",
		"",
	}, "\n")
	if w.stdout.String() != want {
		t.Errorf("stdout =\n%s\nwant\n%s", w.stdout.String(), want)
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", w.stderr.String())
	}

	// Another target has its own cache state.
	w.reset()
	if err := w.a.Deps(ctx, DepsRequest{Common: Common{Target: "ios_simulator_arm64"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:UserNotifications:2.4.21 (ios_simulator_arm64, dump cached: no)\n") {
		t.Errorf("stdout = %q", w.stdout.String())
	}
}

func TestDepsSkipsWhatIsNotAvailable(t *testing.T) {
	// No Kotlin/Native: the SDKs are still listed, and the reason is on stderr.
	w := newWorld(t)
	w.a.UserHome = t.TempDir()
	if err := w.a.Deps(context.Background(), DepsRequest{}); err != nil {
		t.Fatalf("Deps() without Kotlin/Native error = %v", err)
	}
	if strings.Contains(w.stdout.String(), "kn.") || strings.Count(w.stdout.String(), "sdk.apple:<Framework>:") != 3 {
		t.Errorf("stdout = %q, want the three SDK lines only", w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), "ksrc-native: skipped: E_NO_KONAN: ") {
		t.Errorf("stderr = %q, want the skipped reason", w.stderr.String())
	}

	// No Xcode: the Kotlin/Native lines are still listed.
	w = newWorld(t)
	w.runner.Fakes = nil
	w.runner.Hidden = []string{"xcrun", "xcode-select"}
	if err := w.a.Deps(context.Background(), DepsRequest{}); err != nil {
		t.Fatalf("Deps() without Xcode error = %v", err)
	}
	if got, want := w.stdout.String(), "kn.platform:UserNotifications:2.4.21 (ios_arm64, dump cached: no)\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(w.stderr.String(), "ksrc-native: skipped: E_NO_XCODE: ") {
		t.Errorf("stderr = %q, want the skipped reason", w.stderr.String())
	}

	// Neither: the first reason is the error.
	w.a.UserHome = t.TempDir()
	w.reset()
	if err := w.a.Deps(context.Background(), DepsRequest{}); errs.Code(err) != errs.NoKonan || w.stdout.Len() != 0 {
		t.Errorf("Deps() with nothing available: error = %v, stdout = %q; want %s", err, w.stdout.String(), errs.NoKonan)
	}

	// A distribution named by a flag is not optional.
	w = newWorld(t)
	for _, common := range []Common{{KotlinVersion: "1.9.0"}, {KonanHome: filepath.Join(t.TempDir(), "nope")}} {
		w.reset()
		if err := w.a.Deps(context.Background(), DepsRequest{Common: common}); errs.Code(err) != errs.NoKonan || w.stdout.Len() != 0 {
			t.Errorf("Deps(%+v) error = %v, stdout = %q; want %s and no output", common, err, w.stdout.String(), errs.NoKonan)
		}
	}

	// Neither is a target the distribution does not have: a list of SDKs with
	// exit code 0 would hide the typo.
	w = newWorld(t)
	w.reset()
	err := w.a.Deps(context.Background(), DepsRequest{Common: Common{Target: "tvos_arm64"}})
	if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), `target "tvos_arm64"`) || w.stdout.Len() != 0 {
		t.Errorf("Deps(--target tvos_arm64) error = %v, stdout = %q; want %s and no output", err, w.stdout.String(), errs.NoSources)
	}

	// Without a distribution the target cannot be checked and is not used.
	w.a.UserHome = t.TempDir()
	w.reset()
	if err := w.a.Deps(context.Background(), DepsRequest{Common: Common{Target: "tvos_arm64"}}); err != nil || strings.Count(w.stdout.String(), "sdk.apple:<Framework>:") != 3 {
		t.Errorf("Deps(--target tvos_arm64) without Kotlin/Native = %v, stdout = %q; want the three SDK lines", err, w.stdout.String())
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fillCache caches one platform dump and the stdlib.
func fillCache(t *testing.T, w *world) {
	t.Helper()
	w.dist.AddStdlibSources(t, nil)
	if err := w.a.Search(context.Background(), SearchRequest{Pattern: "x", Modules: []string{unModule, stdlibModule}}); err != nil {
		t.Fatal(err)
	}
	w.reset()
}

func cacheEntries(w *world) int {
	store := cache.Store{Root: w.env[cache.EnvDir]}
	return len(store.List(provider.PlatformNamespace)) + len(store.List(provider.StdlibNamespace))
}

func TestCleanDryRunAndNoTerminal(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.a.Clean(ctx, CleanRequest{}); err != nil {
		t.Fatalf("Clean() on an empty cache error = %v", err)
	}
	if got, want := w.stdout.String(), "the cache is empty: "+w.env[cache.EnvDir]+"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	fillCache(t, w)
	for name, req := range map[string]CleanRequest{
		"--dry-run":              {DryRun: true},
		"--dry-run wins over -y": {DryRun: true, Yes: true},
	} {
		w.reset()
		if err := w.a.Clean(ctx, req); err != nil {
			t.Fatalf("Clean(%s) error = %v", name, err)
		}
		out := w.stdout.String()
		for _, part := range []string{
			"kn.platform:UserNotifications:2.4.21 (ios_arm64) ",
			"kn.stdlib:native:2.4.21 ",
			"total: 2 entries, ",
			" in " + w.env[cache.EnvDir] + "\n",
			"dry run: nothing deleted\n",
		} {
			if !strings.Contains(out, part) {
				t.Errorf("Clean(%s) output lacks %q:\n%s", name, part, out)
			}
		}
		if cacheEntries(w) != 2 {
			t.Fatalf("Clean(%s) deleted entries", name)
		}
	}

	// No terminal and no -y: a dry run, even with "y" waiting on stdin.
	w.reset()
	w.a.Stdin = strings.NewReader("y\n")
	if err := w.a.Clean(ctx, CleanRequest{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.stdout.String(), "dry run (not a terminal): nothing deleted; pass -y to delete\n") || strings.Contains(w.stdout.String(), "[y/N]") {
		t.Errorf("stdout = %q, want a dry-run note and no prompt", w.stdout.String())
	}
	if cacheEntries(w) != 2 {
		t.Error("Clean() without a terminal deleted entries")
	}
}

func TestCleanAsksOnATerminal(t *testing.T) {
	w := newWorld(t)
	fillCache(t, w)
	w.a.IsTerminal = func() bool { return true }
	ctx := context.Background()

	for _, answer := range []string{"\n", "n\n", "maybe\n", ""} {
		w.reset()
		w.a.Stdin = strings.NewReader(answer)
		if err := w.a.Clean(ctx, CleanRequest{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(w.stdout.String(), "Delete 2 entries (") || !strings.HasSuffix(w.stdout.String(), "? [y/N] nothing deleted\n") {
			t.Errorf("answer %q: stdout = %q, want the prompt and no deletion", answer, w.stdout.String())
		}
		if cacheEntries(w) != 2 {
			t.Fatalf("answer %q deleted entries", answer)
		}
	}

	w.reset()
	w.a.Stdin = strings.NewReader("Y\n")
	if err := w.a.Clean(ctx, CleanRequest{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.stdout.String(), "deleted 2 entries, ") || cacheEntries(w) != 0 {
		t.Errorf("answer Y: stdout = %q, %d entries left", w.stdout.String(), cacheEntries(w))
	}
}

func TestCleanYesDeletesOnlyItsNamespaces(t *testing.T) {
	w := newWorld(t)
	fillCache(t, w)
	root := w.env[cache.EnvDir]
	// A file and a directory that do not belong to the tool, and a leftover of
	// an interrupted build.
	testutil.WriteFile(t, filepath.Join(root, "notes.txt"), []byte("keep me"), 0o644)
	testutil.WriteFile(t, filepath.Join(root, "other-tool", "data.bin"), []byte("keep me too"), 0o644)
	testutil.WriteFile(t, filepath.Join(root, provider.PlatformNamespace, ".tmp-123", "src", "Half.kt"), []byte("half"), 0o644)

	if err := w.a.Clean(context.Background(), CleanRequest{Yes: true}); err != nil {
		t.Fatalf("Clean(-y) error = %v", err)
	}
	if !strings.Contains(w.stdout.String(), "deleted 2 entries, ") || strings.Contains(w.stdout.String(), "[y/N]") {
		t.Errorf("stdout = %q, want a deletion without a prompt", w.stdout.String())
	}
	for _, gone := range []string{provider.PlatformNamespace, provider.StdlibNamespace} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (%v)", gone, err)
		}
	}
	for _, kept := range []string{"notes.txt", filepath.Join("other-tool", "data.bin")} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Errorf("%s was deleted: %v", kept, err)
		}
	}

	// The next search rebuilds.
	if err := w.a.Search(context.Background(), SearchRequest{Pattern: "val UNErrorDomain", Modules: []string{unModule}}); err != nil {
		t.Errorf("Search() after clean error = %v", err)
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 2 {
		t.Errorf("klib ran %d times, want 2 (before and after clean)", len(calls))
	}
}

func TestDoctorWithStdlibAndOptionalParts(t *testing.T) {
	w := newWorld(t)
	zipPath := w.dist.AddStdlibSources(t, nil)
	ctx := context.Background()

	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.stdout.String(), "stdlib: ok - "+zipPath+" (extracted: no)\n") {
		t.Errorf("doctor output:\n%s", w.stdout.String())
	}
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{stdlibModule}}); err != nil {
		t.Fatal(err)
	}
	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{Common: Common{SDK: "macosx"}}); err != nil {
		t.Fatal(err)
	}
	out := w.stdout.String()
	if !strings.Contains(out, "(extracted: yes)") || !strings.Contains(out, "xcode: ok - SDK macosx26.4 at ") {
		t.Errorf("doctor output:\n%s", out)
	}

	// Missing optional parts do not fail doctor.
	w.runner.Fakes = map[string]func(context.Context, executil.Command) error{"java": testutil.FakeJava}
	w.runner.Hidden = []string{"xcrun", "xcode-select"}
	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Errorf("Doctor() without Xcode error = %v, want nil because Xcode is optional", err)
	}
	if !strings.Contains(w.stdout.String(), "xcode: unavailable - E_NO_XCODE: ") {
		t.Errorf("doctor output:\n%s", w.stdout.String())
	}
}

func TestWithOutputRedirectsOneCall(t *testing.T) {
	w := newWorld(t)
	var out, errOut strings.Builder
	clone := w.a.WithOutput(&out, &errOut)

	if err := clone.Where(context.Background(), WhereRequest{Coordinate: "sdk.apple:UIKit"}); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 || w.stdout.Len() != 0 {
		t.Errorf("clone wrote %q, original got %q; want the output in the clone only", out.String(), w.stdout.String())
	}
	if clone.Runner != w.a.Runner || clone.UserHome != w.a.UserHome {
		t.Error("the clone lost the environment of the original")
	}
}
