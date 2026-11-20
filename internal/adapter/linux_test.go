package adapter

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// newLinuxWorld is a Linux machine: a distribution for linux-x86_64 with the
// given targets, each with one library, the stdlib sources, and no Xcode. A
// run of xcrun or xcode-select fails the test.
func newLinuxWorld(t *testing.T, targets ...string) *world {
	t.Helper()
	testutil.RequireRipgrep(t)

	home := t.TempDir()
	w := &world{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	w.dist = testutil.NewDistributionFor(t, filepath.Join(home, ".konan"), "linux-x86_64", "2.4.21")
	for _, target := range targets {
		w.dist.AddPlatformKlib(t, target, "posix", testutil.ReadFixture(t, testutil.UserNotificationsDump))
	}
	w.dist.AddStdlibSources(t, nil)

	apple := func(_ context.Context, cmd executil.Command) error {
		t.Errorf("%s %v was run on Linux", filepath.Base(cmd.Path), cmd.Args)
		return &executil.ExitError{Path: cmd.Path, Code: 1}
	}
	w.runner = &testutil.Overlay{Base: executil.OS{}, Fakes: map[string]func(context.Context, executil.Command) error{
		"java":         testutil.FakeJava,
		"xcrun":        apple,
		"xcode-select": apple,
	}}
	w.env = map[string]string{cache.EnvDir: filepath.Join(t.TempDir(), "cache")}
	w.a = &Adapter{
		Runner:     w.runner,
		Stdout:     w.stdout,
		Stderr:     w.stderr,
		Stdin:      strings.NewReader(""),
		Getenv:     func(key string) string { return w.env[key] },
		UserHome:   home,
		IsTerminal: func() bool { return false },
		GOOS:       "linux",
		GOARCH:     "amd64",
	}
	return w
}

func TestDoctorOnLinuxIsHealthyWithoutIOSAndXcode(t *testing.T) {
	w := newLinuxWorld(t, "linux_x64", "linux_arm64", "android_arm64")
	if err := w.a.Doctor(context.Background(), DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() on a healthy Linux install: error = %v\n%s", err, w.stdout.String())
	}
	out := w.stdout.String()
	if got, want := doctorLine(t, out, "platform"), "platform: ok - 1 library for linux_x64, the host target: the distribution has no ios_arm64 (3 targets in the distribution)"; got != want {
		t.Errorf("platform line = %q\nwant            %q", got, want)
	}
	if got, want := doctorLine(t, out, "xcode"), "xcode: unavailable - Apple SDKs come with Xcode, which runs on macOS only; sdk.apple cannot be searched on linux"; got != want {
		t.Errorf("xcode line = %q\nwant         %q", got, want)
	}
	if strings.Contains(out, "MISSING") {
		t.Errorf("Doctor() on a healthy Linux install prints MISSING:\n%s", out)
	}

	// A target the caller names is not replaced.
	w.reset()
	err := w.a.Doctor(context.Background(), DoctorRequest{Common: Common{Target: "ios_arm64"}})
	if errs.Code(err) != DoctorFailed || !strings.HasPrefix(doctorLine(t, w.stdout.String(), "platform"), "platform: MISSING - no platform libraries for target ios_arm64") {
		t.Errorf("Doctor(--target ios_arm64) on Linux: error = %v, platform line = %q; want E_DOCTOR and MISSING", err, doctorLine(t, w.stdout.String(), "platform"))
	}
}

func TestDoctorOnMacNamesNoHostTarget(t *testing.T) {
	w := newWorld(t)
	if err := w.a.Doctor(context.Background(), DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if got, want := doctorLine(t, w.stdout.String(), "platform"), "platform: ok - 1 library for ios_arm64 (2 targets in the distribution)"; got != want {
		t.Errorf("platform line = %q, want %q", got, want)
	}
}

func TestDepsOnLinux(t *testing.T) {
	t.Run("lists the host target and the stdlib, and says nothing about Xcode", func(t *testing.T) {
		w := newLinuxWorld(t, "linux_x64", "linux_arm64")
		if err := w.a.Deps(context.Background(), DepsRequest{}); err != nil {
			t.Fatalf("Deps() error = %v", err)
		}
		lines := strings.Split(strings.TrimRight(w.stdout.String(), "\n"), "\n")
		if len(lines) != 2 || lines[0] != "kn.platform:posix:2.4.21 (linux_x64, dump cached: no)" || !strings.HasPrefix(lines[1], "kn.stdlib:native:2.4.21 (extracted: no, ") {
			t.Errorf("Deps() stdout = %q, want the posix line of linux_x64 and the stdlib line", w.stdout.String())
		}
		if w.stderr.Len() != 0 {
			t.Errorf("Deps() stderr = %q, want nothing: a missing Xcode is no news on Linux", w.stderr.String())
		}
	})

	t.Run("prints the stdlib line when no target can be picked", func(t *testing.T) {
		w := newLinuxWorld(t, "android_arm64")
		if err := w.a.Deps(context.Background(), DepsRequest{}); err != nil {
			t.Fatalf("Deps() error = %v", err)
		}
		if got := w.stdout.String(); !strings.HasPrefix(got, "kn.stdlib:native:2.4.21 (") || strings.Count(got, "\n") != 1 {
			t.Errorf("Deps() stdout = %q, want only the stdlib line", got)
		}
		if got := w.stderr.String(); !strings.HasPrefix(got, "ksrc-native: skipped: E_NO_SOURCES: ") || !strings.Contains(got, `target "ios_arm64"`) || strings.Count(got, "\n") != 1 {
			t.Errorf("Deps() stderr = %q, want one note about the platform libraries", got)
		}
	})

	t.Run("a target the caller names is not replaced", func(t *testing.T) {
		w := newLinuxWorld(t, "linux_x64")
		err := w.a.Deps(context.Background(), DepsRequest{Common: Common{Target: "ios_arm64"}})
		if errs.Code(err) != errs.NoSources || w.stdout.Len() != 0 {
			t.Errorf("Deps(--target ios_arm64) = %v, stdout %q; want %s and no output", err, w.stdout.String(), errs.NoSources)
		}
	})
}

func TestDepsOnMacStillListsTheSDKs(t *testing.T) {
	w := newWorld(t)
	if err := w.a.Deps(context.Background(), DepsRequest{}); err != nil {
		t.Fatalf("Deps() error = %v", err)
	}
	if !strings.Contains(w.stdout.String(), "sdk.apple:<Framework>:iphoneos26.4 (") {
		t.Errorf("Deps() on macOS lists no SDK:\n%s", w.stdout.String())
	}
}

func TestSearchOnLinuxUsesTheHostTarget(t *testing.T) {
	w := newLinuxWorld(t, "linux_x64", "linux_arm64")
	ctx := context.Background()

	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain", Modules: []string{"kn.platform:posix"}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got, want := w.stdout.String(), "kn.platform:posix:2.4.21!/linux_x64/_toplevel.kt 14:14:public final val UNErrorDomain: kotlin/String?\n"; got != want {
		t.Errorf("stdout = %q\nwant     %q", got, want)
	}
	if !strings.Contains(w.stderr.String(), "dumping kn.platform:posix:2.4.21 for linux_x64") {
		t.Errorf("stderr = %q, want a dump for linux_x64", w.stderr.String())
	}

	// The no-matches note names the target that was searched.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "zzzNoSuchSymbol", Modules: []string{"kn.platform:posix"}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got, want := w.stderr.String(), "ksrc-native: no matches for \"zzzNoSuchSymbol\" in kn.platform:posix:2.4.21 (linux_x64)\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}

	// Without a module: the cached dumps of the host target and the stdlib.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "val UNErrorDomain|class MemScope"}); err != nil {
		t.Fatalf("Search() without a module: error = %v", err)
	}
	for _, part := range []string{"kn.platform:posix:2.4.21!/linux_x64/_toplevel.kt ", "kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt "} {
		if !strings.Contains(w.stdout.String(), part) {
			t.Errorf("Search() without a module: stdout lacks %q:\n%s", part, w.stdout.String())
		}
	}

	// A target the caller names is used as given.
	w.reset()
	if err := w.a.Search(ctx, SearchRequest{Common: Common{Target: "linux_arm64"}, Pattern: "val UNErrorDomain", Modules: []string{"kn.platform:posix"}}); err != nil {
		t.Fatalf("Search(--target linux_arm64) error = %v", err)
	}
	if !strings.HasPrefix(w.stdout.String(), "kn.platform:posix:2.4.21!/linux_arm64/_toplevel.kt ") {
		t.Errorf("Search(--target linux_arm64) stdout = %q, want a hit in linux_arm64", w.stdout.String())
	}
	w.reset()
	err := w.a.Search(ctx, SearchRequest{Common: Common{Target: "ios_arm64"}, Pattern: "x", Modules: []string{"kn.platform:posix"}})
	if errs.Code(err) != errs.NoSources {
		t.Errorf("Search(--target ios_arm64) on Linux = %v, want %s", err, errs.NoSources)
	}
}

// An sdk.apple module needs no Kotlin/Native distribution, and asking for
// its target must not start a search for one.
func TestAnAppleModuleDoesNotLookForADistribution(t *testing.T) {
	w := newWorld(t)
	project := t.TempDir()
	testutil.WriteFile(t, filepath.Join(project, "gradle", "libs.versions.toml"), []byte("[versions]\nkotlin = \"2.9.9\"\n"), 0o644)
	err := w.a.Search(context.Background(), SearchRequest{Common: Common{Project: project}, Pattern: "interface UIView ", Modules: []string{"sdk.apple:UIKit"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing: the note about Kotlin 2.9.9 means the distribution was looked up", w.stderr.String())
	}
}
