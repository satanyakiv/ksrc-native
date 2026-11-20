package provider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/dump"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

type fixture struct {
	fake     testutil.Distribution
	platform *Platform
	log      *bytes.Buffer
}

// newFixture builds a fake 2.4.21 distribution with UserNotifications for two
// targets, a library whose dump is empty and one the fake klib cannot dump.
func newFixture(t *testing.T) fixture {
	t.Helper()
	fake := testutil.NewDistribution(t, t.TempDir(), "2.4.21")
	dump := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	fake.AddPlatformKlib(t, "ios_arm64", "UserNotifications", dump)
	fake.AddPlatformKlib(t, "ios_simulator_arm64", "UserNotifications", dump)
	fake.AddPlatformKlib(t, "ios_arm64", "Empty", []byte("library {\n  library fragment {\n    // package name: platform.Empty\n\n    package {\n    }\n  }\n}\n"))
	fake.AddPlatformKlib(t, "ios_arm64", "Broken", nil)

	log := &bytes.Buffer{}
	return fixture{
		fake: fake,
		log:  log,
		platform: &Platform{
			Dist:   konan.Distribution{Home: fake.Home, Version: fake.Version},
			Runner: executil.OS{},
			Cache:  cache.Store{Root: filepath.Join(t.TempDir(), "cache")},
			Log:    log,
		},
	}
}

func coord(t *testing.T, s string) Coordinate {
	t.Helper()
	c, err := ParseCoordinate(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlatformResolveBuildsAndCaches(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The framework name is matched without regard to case and the version is
	// filled in.
	src, err := f.platform.Resolve(ctx, Request{Coordinate: coord(t, "kn.platform:usernotifications"), Target: "ios_arm64"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := src.Coordinate.String(), "kn.platform:UserNotifications:2.4.21"; got != want {
		t.Errorf("Coordinate = %s, want %s", got, want)
	}
	if !strings.HasSuffix(src.Origin, "org.jetbrains.kotlin.native.platform.UserNotifications") {
		t.Errorf("Origin = %s, want the klib directory", src.Origin)
	}

	data, err := os.ReadFile(filepath.Join(src.Root, "ios_arm64", "UNNotification.kt"))
	if err != nil {
		t.Fatalf("the class file is missing: %v", err)
	}
	if !strings.Contains(string(data), "public open class platform/UserNotifications/UNNotification : ") {
		t.Errorf("UNNotification.kt does not hold the class header:\n%.200s", data)
	}
	if _, err := os.Stat(filepath.Join(src.Root, "ios_arm64", "_toplevel.kt")); err != nil {
		t.Errorf("_toplevel.kt is missing: %v", err)
	}
	if got := src.FileID("ios_arm64/UNNotification.kt"); got != "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt" {
		t.Errorf("FileID = %s", got)
	}
	if !strings.Contains(f.log.String(), "dumping kn.platform:UserNotifications:2.4.21 for ios_arm64") {
		t.Errorf("log = %q, want a progress line", f.log.String())
	}

	// A second resolve, with the version spelled out, reads the cache.
	f.log.Reset()
	again, err := f.platform.Resolve(ctx, Request{Coordinate: coord(t, "kn.platform:UserNotifications:2.4.21"), Target: "ios_arm64"})
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if again.Root != src.Root {
		t.Errorf("second Root = %s, want %s", again.Root, src.Root)
	}
	if calls := f.fake.KlibCalls(t); len(calls) != 1 {
		t.Errorf("klib ran %d times, want 1: %q", len(calls), calls)
	}
	if f.log.Len() != 0 {
		t.Errorf("a cached resolve logged %q", f.log.String())
	}
}

func TestPlatformResolveKeepsTargetsApart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c := coord(t, "kn.platform:UserNotifications")

	device, err := f.platform.Resolve(ctx, Request{Coordinate: c, Target: "ios_arm64"})
	if err != nil {
		t.Fatal(err)
	}
	simulator, err := f.platform.Resolve(ctx, Request{Coordinate: c, Target: "ios_simulator_arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if device.Root == simulator.Root {
		t.Error("two targets share one cache entry")
	}
	if _, err := os.Stat(filepath.Join(simulator.Root, "ios_simulator_arm64", "UNNotification.kt")); err != nil {
		t.Errorf("the simulator file is missing: %v", err)
	}

	// An empty target means the default one.
	def, err := f.platform.Resolve(ctx, Request{Coordinate: c})
	if err != nil {
		t.Fatal(err)
	}
	if def.Root != device.Root {
		t.Errorf("default target Root = %s, want the ios_arm64 entry %s", def.Root, device.Root)
	}
}

func TestPlatformResolveErrors(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name   string
		coord  Coordinate
		target string
		code   string
		part   string
	}{
		{"foreign group", Coordinate{Group: "sdk.apple", Name: "UIKit"}, "ios_arm64", errs.BadCoord, "cannot resolve sdk.apple:UIKit"},
		{"version of another distribution", coord(t, "kn.platform:UserNotifications:2.4.10"), "ios_arm64", errs.BadCoord, "the distribution in use is 2.4.21"},
		{"unknown framework", coord(t, "kn.platform:Nope"), "ios_arm64", errs.NoSources, `no platform library "Nope"`},
		{"unknown target", coord(t, "kn.platform:UserNotifications"), "tvos_arm64", errs.NoSources, `target "tvos_arm64"`},
		{"library without declarations", coord(t, "kn.platform:Empty"), "ios_arm64", errs.NoSources, "has no declarations"},
		{"klib tool fails", coord(t, "kn.platform:Broken"), "ios_arm64", errs.KlibFailed, "library not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.platform.Resolve(context.Background(), Request{Coordinate: tt.coord, Target: tt.target})
			if errs.Code(err) != tt.code {
				t.Fatalf("Resolve() error = %v, want %s", err, tt.code)
			}
			if !strings.Contains(err.Error(), tt.part) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.part)
			}
		})
	}

	// Failed builds leave nothing in the cache.
	if got := f.platform.Cache.List(PlatformNamespace); len(got) != 0 {
		t.Errorf("failed resolves left %d cache entries", len(got))
	}
}

func TestPlatformCached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if got := f.platform.Cached("ios_arm64"); len(got) != 0 {
		t.Fatalf("Cached() on an empty cache = %v", got)
	}

	c := coord(t, "kn.platform:UserNotifications")
	device, err := f.platform.Resolve(ctx, Request{Coordinate: c, Target: "ios_arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.platform.Resolve(ctx, Request{Coordinate: c, Target: "ios_simulator_arm64"}); err != nil {
		t.Fatal(err)
	}

	got := f.platform.Cached("")
	if len(got) != 1 || got[0].Root != device.Root || got[0].Coordinate.String() != "kn.platform:UserNotifications:2.4.21" {
		t.Errorf("Cached(default target) = %+v, want the ios_arm64 entry only", got)
	}

	// Another distribution does not see these entries, even with the same
	// cache directory.
	otherFake := testutil.NewDistribution(t, t.TempDir(), "2.4.21")
	other := &Platform{Dist: konan.Distribution{Home: otherFake.Home, Version: "2.4.21"}, Runner: executil.OS{}, Cache: f.platform.Cache}
	if got := other.Cached("ios_arm64"); len(got) != 0 {
		t.Errorf("another distribution sees %d entries, want 0", len(got))
	}
	newer := &Platform{Dist: konan.Distribution{Home: f.fake.Home, Version: "2.5.0"}, Runner: executil.OS{}, Cache: f.platform.Cache}
	if got := newer.Cached("ios_arm64"); len(got) != 0 {
		t.Errorf("another version sees %d entries, want 0", len(got))
	}
}

func TestPlatformKeyFollowsTheDumpFormat(t *testing.T) {
	f := newFixture(t)
	klib, err := f.platform.Dist.FindPlatformKlib("ios_arm64", "UserNotifications")
	if err != nil {
		t.Fatal(err)
	}
	if platformKeyFor(klib.Path, "1") == platformKeyFor(klib.Path, "2") {
		t.Error("two dump formats share a cache key: a splitter fix would keep serving the old files")
	}
	if platformKey(klib.Path) != platformKeyFor(klib.Path, dump.FormatVersion) {
		t.Error("platformKey does not use dump.FormatVersion")
	}

	// A built entry records the format it was split with.
	if _, err := f.platform.Resolve(context.Background(), Request{Coordinate: coord(t, "kn.platform:UserNotifications"), Target: "ios_arm64"}); err != nil {
		t.Fatal(err)
	}
	entry, ok := f.platform.Cache.Get(PlatformNamespace, platformKey(klib.Path))
	if !ok || entry.Meta.Format != dump.FormatVersion {
		t.Errorf("the built entry has format %q (found: %v), want %q", entry.Meta.Format, ok, dump.FormatVersion)
	}
}

// An entry that another version of the splitter wrote stays on disk until
// clean, but a search without --module must not read it next to the new one.
func TestPlatformCachedSkipsEntriesOfAnotherDumpFormat(t *testing.T) {
	f := newFixture(t)
	klib, err := f.platform.Dist.FindPlatformKlib("ios_arm64", "UserNotifications")
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.platform.Resolve(context.Background(), Request{Coordinate: coord(t, "kn.platform:UserNotifications"), Target: "ios_arm64"})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"", "0"} {
		_, err := f.platform.Cache.Ensure(PlatformNamespace, platformKeyFor(klib.Path, "old"+format), func(srcDir string) (cache.Meta, error) {
			err := os.WriteFile(filepath.Join(srcDir, "Old.kt"), []byte("old\n"), 0o644)
			return cache.Meta{Coordinate: "kn.platform:UserNotifications:2.4.21", Target: "ios_arm64", Origin: klib.Path, Files: 1, Format: format}, err
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	cached := f.platform.Cached("ios_arm64")
	if len(cached) != 1 || cached[0].Root != current.Root {
		t.Errorf("Cached() = %+v, want only the entry of the current dump format at %s", cached, current.Root)
	}
}

// The format is part of the key, so an entry of an older format is never
// read again. Left on disk it shows up as a second line for the same library
// in clean and in the count of doctor.
func TestPlatformResolveRemovesTheEntryOfAnotherDumpFormat(t *testing.T) {
	f := newFixture(t)
	klib, err := f.platform.Dist.FindPlatformKlib("ios_arm64", "UserNotifications")
	if err != nil {
		t.Fatal(err)
	}
	old := func(key, origin, target, format string) cache.Entry {
		t.Helper()
		e, err := f.platform.Cache.Ensure(PlatformNamespace, key, func(srcDir string) (cache.Meta, error) {
			err := os.WriteFile(filepath.Join(srcDir, "Old.kt"), []byte("old\n"), 0o644)
			return cache.Meta{Coordinate: "kn.platform:UserNotifications:2.4.21", Target: target, Origin: origin, Files: 1, Format: format}, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	gone := []cache.Entry{
		old(platformKeyFor(klib.Path, "none"), klib.Path, "ios_arm64", ""),
		old(platformKeyFor(klib.Path, "0"), klib.Path, "ios_arm64", "0"),
	}
	kept := []cache.Entry{
		old(cache.Key("another distribution"), "/elsewhere/klib/UserNotifications", "ios_arm64", "0"),
		old(cache.Key("another target"), klib.Path, "ios_simulator_arm64", "0"),
	}

	request := Request{Coordinate: coord(t, "kn.platform:UserNotifications"), Target: "ios_arm64"}
	current, err := f.platform.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	check := func(when string) {
		t.Helper()
		for _, e := range gone {
			if _, err := os.Stat(e.Dir); !os.IsNotExist(err) {
				t.Errorf("%s: the entry of dump format %q is still at %s (%v)", when, e.Meta.Format, e.Dir, err)
			}
		}
		for _, e := range kept {
			if _, err := os.Stat(filepath.Join(e.Src, "Old.kt")); err != nil {
				t.Errorf("%s: the entry with origin %s and target %s was removed: %v", when, e.Meta.Origin, e.Meta.Target, err)
			}
		}
		if _, err := os.Stat(current.Root); err != nil {
			t.Errorf("%s: the current entry is gone: %v", when, err)
		}
		if got := len(f.platform.Cache.List(PlatformNamespace)); got != 1+len(kept) {
			t.Errorf("%s: the cache lists %d entries, want %d", when, got, 1+len(kept))
		}
	}
	check("after the build")
	if !strings.Contains(f.log.String(), "removed 2 cache entries of another dump format") {
		t.Errorf("log = %q, want a note about the 2 removed entries", f.log.String())
	}

	// An old entry that appears next to a current one (an older binary still
	// in use) goes with the next use, without a build.
	gone = []cache.Entry{old(platformKeyFor(klib.Path, "0"), klib.Path, "ios_arm64", "0")}
	f.log.Reset()
	if _, err := f.platform.Resolve(context.Background(), request); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	check("after a use without a build")
	if strings.Contains(f.log.String(), "dumping") || !strings.Contains(f.log.String(), "removed 1 cache entry of another dump format") {
		t.Errorf("log = %q, want no build and a note about 1 removed entry", f.log.String())
	}
}
