package provider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func newStdlib(t *testing.T) (*Stdlib, testutil.Distribution, *bytes.Buffer) {
	t.Helper()
	fake := testutil.NewDistribution(t, t.TempDir(), "2.4.21")
	log := &bytes.Buffer{}
	return &Stdlib{
		Dist:  konan.Distribution{Home: fake.Home, Version: fake.Version},
		Cache: cache.Store{Root: filepath.Join(t.TempDir(), "cache")},
		Log:   log,
	}, fake, log
}

func stdlibRequest(t *testing.T, s string) Request {
	t.Helper()
	return Request{Coordinate: coord(t, s)}
}

func TestStdlibResolveExtractsOnce(t *testing.T) {
	s, fake, log := newStdlib(t)
	zipPath := fake.AddStdlibSources(t, map[string]string{
		"nativeMain/":                          "",
		"nativeMain/kotlinx/cinterop/Utils.kt": testutil.StdlibUtils,
		"commonMain/kotlin/Unit.kt":            "package kotlin\n\npublic object Unit\n",
	})
	ctx := context.Background()

	if !s.Available() || s.Cached() {
		t.Fatalf("Available() = %v, Cached() = %v; want true and false before the first use", s.Available(), s.Cached())
	}

	src, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:native"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := src.Coordinate.String(); got != "kn.stdlib:native:2.4.21" {
		t.Errorf("Coordinate = %s", got)
	}
	if src.Origin != zipPath {
		t.Errorf("Origin = %s, want the zip %s", src.Origin, zipPath)
	}
	data, err := os.ReadFile(filepath.Join(src.Root, "nativeMain", "kotlinx", "cinterop", "Utils.kt"))
	if err != nil || string(data) != testutil.StdlibUtils {
		t.Errorf("Utils.kt = %q, %v", data, err)
	}
	if got := src.FileID("nativeMain/kotlinx/cinterop/Utils.kt"); got != "kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt" {
		t.Errorf("FileID = %s", got)
	}
	if !strings.Contains(log.String(), "extracting kn.stdlib:native:2.4.21") {
		t.Errorf("log = %q, want a progress line", log.String())
	}
	if !s.Cached() {
		t.Error("Cached() = false after the first use")
	}
	entries := s.Cache.List(StdlibNamespace)
	if len(entries) != 1 || entries[0].Meta.Files != 2 || entries[0].Meta.Coordinate != "kn.stdlib:native:2.4.21" {
		t.Errorf("cache entries = %+v, want one entry with 2 files", entries)
	}

	// The second resolve, with the version and another case, reads the cache.
	log.Reset()
	again, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:Native:2.4.21"))
	if err != nil || again.Root != src.Root || log.Len() != 0 {
		t.Errorf("second Resolve() = %+v, %v, log %q; want the cached root and no log", again, err, log.String())
	}

	// A replaced zip gets a new entry.
	fake.AddStdlibSources(t, map[string]string{"nativeMain/kotlin/New.kt": "package kotlin\n"})
	if err := os.Chtimes(zipPath, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	replaced, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:native"))
	if err != nil || replaced.Root == src.Root {
		t.Errorf("Resolve() after the zip changed = %+v, %v; want a new cache entry", replaced, err)
	}
	if _, err := os.Stat(filepath.Join(replaced.Root, "nativeMain", "kotlin", "New.kt")); err != nil {
		t.Errorf("the new entry lacks the new file: %v", err)
	}
}

func TestStdlibLocate(t *testing.T) {
	s, fake, _ := newStdlib(t)
	zipPath := fake.AddStdlibSources(t, nil)

	loc, err := s.Locate(context.Background(), stdlibRequest(t, "kn.stdlib:native"))
	if err != nil || loc.Path != zipPath || loc.Coordinate.String() != "kn.stdlib:native:2.4.21" {
		t.Errorf("Locate() = %+v, %v; want the zip path", loc, err)
	}
	if s.Cached() {
		t.Error("Locate() extracted the sources")
	}
}

func TestStdlibErrors(t *testing.T) {
	s, fake, _ := newStdlib(t)
	ctx := context.Background()

	// No zip in the distribution.
	if s.Available() {
		t.Fatal("Available() = true without a zip")
	}
	for name, call := range map[string]func() error{
		"Resolve": func() error { _, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:native")); return err },
		"Locate":  func() error { _, err := s.Locate(ctx, stdlibRequest(t, "kn.stdlib:native")); return err },
	} {
		if err := call(); errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "has no stdlib sources") {
			t.Errorf("%s without a zip: error = %v, want %s", name, err, errs.NoSources)
		}
	}

	fake.AddStdlibSources(t, nil)
	tests := []struct {
		name, coord, code, part string
	}{
		{"foreign group", "kn.platform:UIKit", errs.BadCoord, "cannot resolve kn.platform:UIKit"},
		{"unknown module", "kn.stdlib:common", errs.NoSources, `has no module "common"`},
		{"version of another distribution", "kn.stdlib:native:2.4.10", errs.BadCoord, "the distribution in use is 2.4.21"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Resolve(ctx, stdlibRequest(t, tt.coord))
			if errs.Code(err) != tt.code || !strings.Contains(err.Error(), tt.part) {
				t.Errorf("Resolve() error = %v, want %s with %q", err, tt.code, tt.part)
			}
		})
	}
}

func TestStdlibRejectsBadArchives(t *testing.T) {
	ctx := context.Background()

	t.Run("not a zip", func(t *testing.T) {
		s, fake, _ := newStdlib(t)
		testutil.WriteFile(t, filepath.Join(fake.Home, "sources", "kotlin-stdlib-native-sources.zip"), []byte("not a zip"), 0o644)
		_, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:native"))
		if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "cannot extract") {
			t.Errorf("Resolve() error = %v, want %s", err, errs.NoSources)
		}
		if len(s.Cache.List(StdlibNamespace)) != 0 {
			t.Error("a failed extraction left a cache entry")
		}
	})

	t.Run("entry that leaves the archive", func(t *testing.T) {
		s, fake, _ := newStdlib(t)
		fake.AddStdlibSources(t, map[string]string{
			"nativeMain/ok.kt":   "ok\n",
			"../../escaped.kt":   "evil\n",
			"nativeMain/late.kt": "late\n",
		})
		_, err := s.Resolve(ctx, stdlibRequest(t, "kn.stdlib:native"))
		if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "points outside the archive") {
			t.Errorf("Resolve() error = %v, want %s for the escaping entry", err, errs.NoSources)
		}
		// Nothing was written next to the cache.
		matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Cache.Root), "*escaped*"))
		more, _ := filepath.Glob(filepath.Join(s.Cache.Root, "*escaped*"))
		if len(matches)+len(more) != 0 {
			t.Errorf("the escaping entry was written: %v %v", matches, more)
		}
	})
}
