package adapter

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/search"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// world is an Adapter over a fake home directory: two fake distributions with
// the UserNotifications excerpt, a fake Xcode with three SDKs, a fake java,
// the real rg and an empty cache. Only rg and the fake klib script run as
// processes.
type world struct {
	a      *Adapter
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	old    testutil.Distribution // Kotlin 2.4.10
	dist   testutil.Distribution // Kotlin 2.4.21, the newest
	xcode  *testutil.Xcode
	runner *testutil.Overlay
	env    map[string]string
}

const viewControllerHeader = `@interface UIViewController : UIResponder
- (void)presentViewController:(UIViewController *)viewControllerToPresent animated:(BOOL)flag completion:(void (^)(void))completion;
@end
`

func newWorld(t *testing.T) *world {
	t.Helper()
	testutil.RequireRipgrep(t)

	home := t.TempDir()
	dump := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	w := &world{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	w.old = testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.10")
	w.old.AddPlatformKlib(t, "ios_arm64", "UserNotifications", dump)
	w.dist = testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.21")
	w.dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", dump)
	w.dist.AddPlatformKlib(t, "ios_simulator_arm64", "UserNotifications", dump)

	w.xcode = testutil.NewXcode(t)
	ios := w.xcode.AddSDK(t, "iPhoneOS", "26.4")
	w.xcode.AddHeader(t, ios, "UIKit", "UIViewController.h", viewControllerHeader)
	w.xcode.AddHeader(t, ios, "UIKit", "UIView.h", "@interface UIView : UIResponder\n@end\n")
	w.xcode.AddSwiftOnlyFramework(t, ios, "SwiftUI")
	sim := w.xcode.AddSDK(t, "iPhoneSimulator", "26.4")
	w.xcode.AddHeader(t, sim, "UIKit", "UIViewController.h", "// simulator SDK\n"+viewControllerHeader)
	mac := w.xcode.AddSDK(t, "MacOSX", "26.4")
	w.xcode.AddVersionedHeader(t, mac, "AppKit", "NSWindow.h", "@interface NSWindow : NSResponder\n@end\n")
	w.runner = &testutil.Overlay{Base: executil.OS{}, Fakes: w.xcode.Fakes()}
	w.runner.Fakes["java"] = testutil.FakeJava // no test needs a JDK

	w.env = map[string]string{cache.EnvDir: filepath.Join(t.TempDir(), "cache")}
	w.a = &Adapter{
		Runner:     w.runner,
		Stdout:     w.stdout,
		Stderr:     w.stderr,
		Stdin:      strings.NewReader(""),
		Getenv:     func(key string) string { return w.env[key] },
		UserHome:   home,
		IsTerminal: func() bool { return false },
		GOOS:       "darwin",
		GOARCH:     "arm64",
	}
	return w
}

func (w *world) reset() {
	w.stdout.Reset()
	w.stderr.Reset()
}

const unModule = "kn.platform:UserNotifications"

func TestSearchPrintsTheOutputContract(t *testing.T) {
	w := newWorld(t)
	err := w.a.Search(context.Background(), SearchRequest{
		Pattern: `class platform/UserNotifications/UNNotification `,
		Modules: []string{unModule},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt 2:13:public open class platform/UserNotifications/UNNotification : platform/darwin/NSObject, platform/Foundation/NSCopyingProtocol, platform/Foundation/NSSecureCodingProtocol {\n"
	if w.stdout.String() != want {
		t.Errorf("stdout = %q\nwant     %q", w.stdout.String(), want)
	}
	if !strings.Contains(w.stderr.String(), "dumping kn.platform:UserNotifications:2.4.21 for ios_arm64") {
		t.Errorf("stderr = %q, want the progress line of the first build", w.stderr.String())
	}

	// The second search reads the cache: no klib run, no progress line.
	w.reset()
	if err := w.a.Search(context.Background(), SearchRequest{Pattern: "val UNErrorDomain", Modules: []string{unModule}}); err != nil {
		t.Fatalf("second Search() error = %v", err)
	}
	if got, want := w.stdout.String(), "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:public final val UNErrorDomain: kotlin/String?\n"; got != want {
		t.Errorf("stdout = %q\nwant     %q", got, want)
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing for a cached search", w.stderr.String())
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 1 {
		t.Errorf("klib ran %d times, want 1", len(calls))
	}
}

func TestSearchWithoutModuleUsesCachedDumps(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	err := w.a.Search(ctx, SearchRequest{Pattern: "UNErrorDomain"})
	if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "--module kn.platform:UIKit") {
		t.Fatalf("Search() without a module on an empty cache: error = %v, want %s with a --module hint", err, errs.NoSources)
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a search without a module built %d dumps, want 0", len(calls))
	}

	// Cache the simulator dump only: the default target still has nothing.
	if err := w.a.Search(ctx, SearchRequest{Common: Common{Target: "ios_simulator_arm64"}, Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	if err := w.a.Search(ctx, SearchRequest{Pattern: "UNErrorDomain"}); errs.Code(err) != errs.NoSources {
		t.Errorf("Search() for ios_arm64 with only a simulator dump cached: error = %v, want %s", err, errs.NoSources)
	}

	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{Target: "ios_simulator_arm64"}, Pattern: "val UNErrorDomain"}); err != nil {
		t.Fatalf("Search() without a module error = %v", err)
	}
	if got, want := w.stdout.String(), "kn.platform:UserNotifications:2.4.21!/ios_simulator_arm64/_toplevel.kt 14:14:public final val UNErrorDomain: kotlin/String?\n"; got != want {
		t.Errorf("stdout = %q\nwant     %q", got, want)
	}
}

func TestSearchNoMatches(t *testing.T) {
	w := newWorld(t)
	if err := w.a.Search(context.Background(), SearchRequest{Pattern: "NoSuchSymbolAnywhere", Modules: []string{unModule}}); err != nil {
		t.Fatalf("Search() error = %v, want nil for no matches", err)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), `no matches for "NoSuchSymbolAnywhere" in kn.platform:UserNotifications:2.4.21 (ios_arm64)`) {
		t.Errorf("stderr = %q, want a no-matches note", w.stderr.String())
	}
}

func TestSearchContextAndRgArgs(t *testing.T) {
	w := newWorld(t)
	err := w.a.Search(context.Background(), SearchRequest{
		Pattern: "COMPANION OBJECT PLATFORM",
		Modules: []string{unModule},
		Context: 1,
		RgArgs:  []string{"-i", "-g", "UNNotification.kt"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(w.stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a context line, the match and a context line:\n%s", len(lines), w.stdout.String())
	}
	const id = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt "
	if !strings.HasPrefix(lines[0], id) || !strings.HasSuffix(lines[0], "-") {
		t.Errorf("line 1 = %q, want an empty context line", lines[0])
	}
	if !strings.Contains(lines[1], ":14:public final companion object platform/UserNotifications/UNNotification.Companion") {
		t.Errorf("line 2 = %q, want the companion match at column 14", lines[1])
	}
	if !strings.HasPrefix(lines[2], id) || !strings.HasSuffix(lines[2], "-") {
		t.Errorf("line 3 = %q, want an empty context line", lines[2])
	}
}

// Context lines that come from -A or -B of rg are groups like those of
// --context, so they get the same "--" between them.
func TestSearchSeparatesTheContextGroupsOfRgArgs(t *testing.T) {
	tests := []struct {
		name   string
		rgArgs []string
		want   bool
	}{
		{"after context", []string{"-A", "1"}, true},
		{"before context", []string{"--before-context=1"}, true},
		{"after context of zero", []string{"-A", "0"}, false},
		{"no context", []string{"-i"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			err := w.a.Search(context.Background(), SearchRequest{
				Pattern: "companion object platform",
				Modules: []string{unModule},
				RgArgs:  tt.rgArgs,
			})
			if err != nil {
				t.Fatalf("Search(%v) error = %v", tt.rgArgs, err)
			}
			out := w.stdout.String()
			if strings.Count(out, ":14:public final companion object") < 2 {
				t.Fatalf("Search(%v) printed fewer than two matches, the test needs two groups:\n%s", tt.rgArgs, out)
			}
			if got := strings.Contains(out, "\n--\n"); got != tt.want {
				t.Errorf("Search(%v) separator between the groups = %v, want %v:\n%s", tt.rgArgs, got, tt.want, out)
			}
		})
	}
}

func TestSearchVersionSelection(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	// A version in the coordinate selects that distribution.
	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain", Modules: []string{unModule + ":2.4.10"}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:UserNotifications:2.4.10!/ios_arm64/_toplevel.kt ") {
		t.Errorf("stdout = %q, want a 2.4.10 file-id", w.stdout.String())
	}
	if len(w.old.KlibCalls(t)) != 1 || len(w.dist.KlibCalls(t)) != 0 {
		t.Error("the dump was not built by the 2.4.10 distribution")
	}

	// So does --kotlin-version, and so does the project's version catalog.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{KotlinVersion: "2.4.10"}, Pattern: "val UNErrorDomain", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:UserNotifications:2.4.10!/") {
		t.Errorf("stdout = %q, want a 2.4.10 file-id", w.stdout.String())
	}
	project := t.TempDir()
	testutil.WriteFile(t, filepath.Join(project, "gradle", "libs.versions.toml"), []byte("[versions]\nkotlin = \"2.4.10\"\n"), 0o644)
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{Project: project}, Pattern: "val UNErrorDomain", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:UserNotifications:2.4.10!/") {
		t.Errorf("stdout = %q, want a 2.4.10 file-id from the version catalog", w.stdout.String())
	}
}

func TestSearchErrors(t *testing.T) {
	w := newWorld(t)
	tests := []struct {
		name string
		req  SearchRequest
		code string
		part string
	}{
		{"empty pattern", SearchRequest{Pattern: "  ", Modules: []string{unModule}}, errs.BadArgs, "pattern is empty"},
		{"negative context", SearchRequest{Pattern: "x", Context: -1}, errs.BadArgs, "--context"},
		{"unparsable module", SearchRequest{Pattern: "x", Modules: []string{"UIKit"}}, errs.BadCoord, `cannot parse the coordinate "UIKit"`},
		{"unknown group", SearchRequest{Pattern: "x", Modules: []string{"com.example:lib"}}, errs.BadCoord, `unknown group "com.example"`},
		{"two versions", SearchRequest{Pattern: "x", Modules: []string{unModule + ":2.4.10", unModule + ":2.4.21"}}, errs.BadCoord, "two Kotlin versions"},
		{"flag disagrees with the coordinate", SearchRequest{Common: Common{KotlinVersion: "2.4.21"}, Pattern: "x", Modules: []string{unModule + ":2.4.10"}}, errs.BadCoord, "disagrees"},
		{"version that is not installed", SearchRequest{Pattern: "x", Modules: []string{unModule + ":1.9.0"}}, errs.NoKonan, "installed: 2.4.21, 2.4.10"},
		{"unknown framework", SearchRequest{Pattern: "x", Modules: []string{"kn.platform:UserNotification"}}, errs.NoSources, "did you mean UserNotifications?"},
		{"bad regular expression", SearchRequest{Pattern: "(", Modules: []string{unModule}}, errs.RgFailed, "-- -F"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			err := w.a.Search(context.Background(), tt.req)
			if errs.Code(err) != tt.code {
				t.Fatalf("Search() error = %v, want %s", err, tt.code)
			}
			if !strings.Contains(err.Error(), tt.part) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.part)
			}
			if !strings.Contains(err.Error(), " Try: ") {
				t.Errorf("error %q has no hint", err.Error())
			}
			if w.stdout.Len() != 0 {
				t.Errorf("a failed search wrote %q to stdout", w.stdout.String())
			}
		})
	}
}

func TestSearchFailsEarlyWithoutRipgrep(t *testing.T) {
	w := newWorld(t)
	w.a.Runner = &testutil.Runner{} // nothing on PATH

	err := w.a.Search(context.Background(), SearchRequest{Pattern: "x", Modules: []string{unModule}})
	if errs.Code(err) != errs.NoRipgrep {
		t.Fatalf("Search() error = %v, want %s", err, errs.NoRipgrep)
	}
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a dump was built although rg is missing")
	}
}

func TestSearchWithoutADistribution(t *testing.T) {
	w := newWorld(t)
	w.a.UserHome = t.TempDir()

	err := w.a.Search(context.Background(), SearchRequest{Pattern: "x", Modules: []string{unModule}})
	if errs.Code(err) != errs.NoKonan || !strings.HasPrefix(err.Error(), "E_NO_KONAN: ") || !strings.Contains(err.Error(), "Try: run a Kotlin/Native Gradle task once") {
		t.Errorf("Search() error = %v, want E_NO_KONAN with the Gradle hint", err)
	}
}

func TestCat(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	const id = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt"

	// cat builds the dump when it is not cached yet.
	if err := w.a.Cat(ctx, CatRequest{FileID: id, Lines: "1,2"}); err != nil {
		t.Fatalf("Cat() error = %v", err)
	}
	want := "@kotlinx/cinterop/ExternalObjCClass\npublic open class platform/UserNotifications/UNNotification : platform/darwin/NSObject, platform/Foundation/NSCopyingProtocol, platform/Foundation/NSSecureCodingProtocol {\n"
	if w.stdout.String() != want {
		t.Errorf("stdout = %q\nwant     %q", w.stdout.String(), want)
	}

	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: id, Lines: "2", Numbers: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stdout.String(), "2\tpublic open class platform/UserNotifications/UNNotification : ") || strings.Count(w.stdout.String(), "\n") != 1 {
		t.Errorf("stdout = %q, want line 2 with its number", w.stdout.String())
	}

	// The whole file, and the same content as the golden copy.
	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: id}); err != nil {
		t.Fatal(err)
	}
	if golden := testutil.ReadFixture(t, "golden/usernotifications/UNNotification.kt"); w.stdout.String() != string(golden) {
		t.Error("cat of the whole file differs from the golden file")
	}

	// The target comes from the file-id, not from --target.
	w.reset()
	err := w.a.Cat(ctx, CatRequest{Common: Common{Target: "ios_arm64"}, FileID: "kn.platform:UserNotifications:2.4.21!/ios_simulator_arm64/_toplevel.kt", Lines: "1"})
	if err != nil {
		t.Fatalf("Cat() of a simulator file-id error = %v", err)
	}
	if !strings.HasPrefix(w.stdout.String(), "public typealias UNNotificationSoundNameVar") {
		t.Errorf("stdout = %q", w.stdout.String())
	}

	// The version of the file-id selects the distribution.
	w.reset()
	if err := w.a.Cat(ctx, CatRequest{FileID: "kn.platform:UserNotifications:2.4.10!/ios_arm64/UNNotification.kt", Lines: "1"}); err != nil {
		t.Fatalf("Cat() of a 2.4.10 file-id error = %v", err)
	}
	if len(w.old.KlibCalls(t)) != 1 {
		t.Error("the 2.4.10 distribution did not build the dump")
	}
}

func TestCatErrors(t *testing.T) {
	w := newWorld(t)
	tests := []struct {
		name string
		req  CatRequest
		code string
		part string
	}{
		{"not a file-id", CatRequest{FileID: "UNNotification.kt"}, errs.BadFileID, "cannot parse the file-id"},
		{"no target segment", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/UNNotification.kt"}, errs.BadFileID, "no target segment"},
		{"unknown group", CatRequest{FileID: "com.example:lib:1.0!/src/Main.kt"}, errs.BadCoord, `unknown group "com.example"`},
		{"bad range", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt", Lines: "9,1"}, errs.BadLines, "cannot parse the line range"},
		{"range past the end", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotificationMeta.kt", Lines: "900,910"}, errs.BadLines, "the file has"},
		{"file that does not exist", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/ios_arm64/Nope.kt"}, errs.NoFile, "has no file ios_arm64/Nope.kt"},
		{"path that leaves the module", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/ios_arm64/../../meta.json"}, errs.BadFileID, "no target segment"},
		{"unknown target", CatRequest{FileID: "kn.platform:UserNotifications:2.4.21!/tvos_arm64/UNNotification.kt"}, errs.NoSources, `target "tvos_arm64"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			err := w.a.Cat(context.Background(), tt.req)
			if errs.Code(err) != tt.code {
				t.Fatalf("Cat() error = %v, want %s", err, tt.code)
			}
			if !strings.Contains(err.Error(), tt.part) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.part)
			}
			if w.stdout.Len() != 0 {
				t.Errorf("a failed cat wrote %q to stdout", w.stdout.String())
			}
		})
	}
}

func TestDoctor(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() error = %v\n%s", err, w.stdout.String())
	}
	out := w.stdout.String()
	for _, part := range []string{
		"rg: ok - ",
		"(ripgrep ",
		"kotlin-native: ok - 2.4.21 at " + w.dist.Home + " (also installed: 2.4.10)",
		"klib: ok - " + filepath.Join(w.dist.Home, "bin", "klib"),
		"platform: ok - 1 libraries for ios_arm64 (2 targets in the distribution)",
		"stdlib: unavailable - no sources zip at ",
		"xcode: ok - SDK iphoneos26.4 at ",
		"(1 frameworks with headers)",
		"cache: ok - " + w.env[cache.EnvDir] + " (0 platform dumps, 0 B)",
	} {
		if !strings.Contains(out, part) {
			t.Errorf("doctor output lacks %q:\n%s", part, out)
		}
	}

	// The cache line counts dumps after a search.
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.stdout.String(), "(1 platform dump, ") {
		t.Errorf("doctor output does not count the cached dump:\n%s", w.stdout.String())
	}
}

func TestDoctorReportsWhatIsMissing(t *testing.T) {
	w := newWorld(t)
	w.a.Runner = &testutil.Runner{} // no rg
	w.a.UserHome = t.TempDir()      // no distribution

	err := w.a.Doctor(context.Background(), DoctorRequest{})
	if errs.Code(err) != DoctorFailed || !strings.Contains(err.Error(), "4 of the checks") {
		t.Fatalf("Doctor() error = %v, want %s for 4 failed checks", err, DoctorFailed)
	}
	out := w.stdout.String()
	for _, part := range []string{
		"rg: MISSING - E_NO_RG: ",
		"kotlin-native: MISSING - E_NO_KONAN: ",
		"klib: MISSING - ",
		"platform: MISSING - ",
		"stdlib: unavailable - ",
		"xcode: unavailable - E_NO_XCODE: ",
		"cache: ok - ",
	} {
		if !strings.Contains(out, part) {
			t.Errorf("doctor output lacks %q:\n%s", part, out)
		}
	}

	// An unknown target fails the platform check only.
	w2 := newWorld(t)
	err = w2.a.Doctor(context.Background(), DoctorRequest{Common: Common{Target: "tvos_arm64"}})
	if errs.Code(err) != DoctorFailed || !strings.Contains(w2.stdout.String(), "platform: MISSING - no platform libraries for target tvos_arm64; available targets: ios_arm64, ios_simulator_arm64") {
		t.Errorf("Doctor(unknown target) error = %v, output:\n%s", err, w2.stdout.String())
	}
}

func TestLinePrinterSeparatesContextGroups(t *testing.T) {
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	p := &linePrinter{out: out, context: true}
	for _, l := range []search.Line{
		{FileID: "a!/x.kt", Number: 1, Text: "ctx"},
		{FileID: "a!/x.kt", Number: 2, Column: 3, Text: "hit", Match: true},
		{FileID: "a!/x.kt", Number: 9, Column: 1, Text: "far hit", Match: true},
		{FileID: "a!/y.kt", Number: 10, Column: 1, Text: "other file", Match: true},
	} {
		if err := p.print(l); err != nil {
			t.Fatal(err)
		}
	}
	out.Flush()
	want := "a!/x.kt 1-ctx\na!/x.kt 2:3:hit\n--\na!/x.kt 9:1:far hit\n--\na!/y.kt 10:1:other file\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}

	// Without context there are no separators.
	buf.Reset()
	p = &linePrinter{out: out}
	p.print(search.Line{FileID: "a!/x.kt", Number: 2, Column: 1, Text: "hit", Match: true})
	p.print(search.Line{FileID: "a!/x.kt", Number: 9, Column: 1, Text: "hit", Match: true})
	out.Flush()
	if strings.Contains(buf.String(), "--") {
		t.Errorf("output without context = %q, want no separator", buf.String())
	}
}

func TestCodedGivesEveryErrorACode(t *testing.T) {
	if coded(nil) != nil {
		t.Error("coded(nil) != nil")
	}
	known := errs.New(errs.NoSources, "x", "y")
	if coded(known) != error(known) {
		t.Error("coded() changed an error that already has a code")
	}
	plain := errors.New("disk on fire")
	got := coded(plain)
	if errs.Code(got) != Internal || !errors.Is(got, plain) || !strings.Contains(got.Error(), "disk on fire") {
		t.Errorf("coded(plain) = %v, want %s wrapping the cause", got, Internal)
	}
}

func TestNewUsesTheRealEnvironment(t *testing.T) {
	a := New(&bytes.Buffer{}, &bytes.Buffer{})
	if a.Runner == nil || a.Getenv == nil || a.UserHome == "" {
		t.Errorf("New() = %+v, want a runner, an environment and a home directory", a)
	}
}
