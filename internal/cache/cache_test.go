package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

func writeOne(name, content string) func(string) (Meta, error) {
	return func(srcDir string) (Meta, error) {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			return Meta{}, err
		}
		return Meta{Coordinate: "kn.platform:UIKit:2.4.21", Target: "ios_arm64", Origin: "/klib/UIKit", Files: 1}, nil
	}
}

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(func(key string) string {
		if key == EnvDir {
			return dir
		}
		return ""
	})
	if err != nil || store.Root != dir {
		t.Errorf("Open(env) = %+v, %v; want root %s", store, err, dir)
	}

	store, err = Open(nil)
	if err != nil {
		t.Fatalf("Open(nil) error = %v", err)
	}
	if filepath.Base(store.Root) != "ksrc-native" {
		t.Errorf("default root = %s, want it to end with ksrc-native", store.Root)
	}
}

func TestKey(t *testing.T) {
	a := Key("/klib/UIKit", "2.4.21")
	if a != Key("/klib/UIKit", "2.4.21") {
		t.Error("Key is not stable")
	}
	if len(a) != 64 {
		t.Errorf("Key length = %d, want 64 hex characters", len(a))
	}
	if a == Key("/klib/UIKit", "2.4.20") {
		t.Error("Key ignores the second part")
	}
	if Key("ab", "c") == Key("a", "bc") {
		t.Error("Key does not separate its parts")
	}
}

func TestEnsureBuildsOnce(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "cache")}
	builds := 0
	build := func(srcDir string) (Meta, error) {
		builds++
		return writeOne("A.kt", "class A\n")(srcDir)
	}

	if _, ok := store.Get("ns-v1", "key"); ok {
		t.Fatal("Get() found an entry in an empty cache")
	}

	first, err := store.Ensure("ns-v1", "key", build)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	second, err := store.Ensure("ns-v1", "key", build)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if builds != 1 {
		t.Errorf("build ran %d times, want 1", builds)
	}
	if first.Src != second.Src || first.Src != filepath.Join(store.Root, "ns-v1", "key", "src") {
		t.Errorf("Src = %s and %s", first.Src, second.Src)
	}
	if data, err := os.ReadFile(filepath.Join(first.Src, "A.kt")); err != nil || string(data) != "class A\n" {
		t.Errorf("A.kt = %q, %v", data, err)
	}
	if second.Meta.Coordinate != "kn.platform:UIKit:2.4.21" || second.Meta.Files != 1 || second.Meta.Created.IsZero() {
		t.Errorf("Meta = %+v", second.Meta)
	}

	// No temporary directories are left behind.
	entries, _ := os.ReadDir(filepath.Join(store.Root, "ns-v1"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temporary directory %s was left behind", e.Name())
		}
	}
}

func TestEnsureFailedBuildLeavesNoEntry(t *testing.T) {
	store := Store{Root: t.TempDir()}
	boom := errors.New("boom")

	_, err := store.Ensure("ns-v1", "key", func(string) (Meta, error) { return Meta{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Ensure() error = %v, want the build error", err)
	}
	if _, ok := store.Get("ns-v1", "key"); ok {
		t.Error("a failed build left a ready entry")
	}
	entries, _ := os.ReadDir(filepath.Join(store.Root, "ns-v1"))
	if len(entries) != 0 {
		t.Errorf("a failed build left %d directories behind", len(entries))
	}

	// The next call builds again.
	if _, err := store.Ensure("ns-v1", "key", writeOne("A.kt", "a\n")); err != nil {
		t.Errorf("Ensure() after a failed build error = %v", err)
	}
}

func TestEnsureReplacesAnEntryWithoutMeta(t *testing.T) {
	store := Store{Root: t.TempDir()}
	leftover := filepath.Join(store.Root, "ns-v1", "key", "src")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "Stale.kt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := store.Ensure("ns-v1", "key", writeOne("Fresh.kt", "fresh\n"))
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(entry.Src, "Stale.kt")); !os.IsNotExist(err) {
		t.Error("the leftover file survived")
	}
	if _, err := os.Stat(filepath.Join(entry.Src, "Fresh.kt")); err != nil {
		t.Errorf("the fresh file is missing: %v", err)
	}
}

func TestEnsureReportsAnUnwritableRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Store{Root: file}.Ensure("ns-v1", "key", writeOne("A.kt", "a\n"))
	if errs.Code(err) != errs.Cache || !strings.Contains(err.Error(), EnvDir) {
		t.Errorf("Ensure() error = %v, want %s with a hint about %s", err, errs.Cache, EnvDir)
	}
}

func TestListAndSize(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "cache")}

	if got := store.List("ns-v1"); len(got) != 0 {
		t.Errorf("List() on an empty cache = %v", got)
	}
	if size, err := store.Size(); err != nil || size != 0 {
		t.Errorf("Size() on an empty cache = %d, %v", size, err)
	}

	meta := func(coord, target string) func(string) (Meta, error) {
		return func(srcDir string) (Meta, error) {
			err := os.WriteFile(filepath.Join(srcDir, "A.kt"), []byte("0123456789"), 0o644)
			return Meta{Coordinate: coord, Target: target}, err
		}
	}
	for _, e := range []struct{ key, coord, target string }{
		{"k1", "kn.platform:UIKit:2.4.21", "ios_simulator_arm64"},
		{"k2", "kn.platform:UIKit:2.4.21", "ios_arm64"},
		{"k3", "kn.platform:Foundation:2.4.21", "ios_arm64"},
	} {
		if _, err := store.Ensure("ns-v1", e.key, meta(e.coord, e.target)); err != nil {
			t.Fatal(err)
		}
	}
	// A directory without meta.json is not an entry.
	if err := os.MkdirAll(filepath.Join(store.Root, "ns-v1", "half"), 0o755); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, e := range store.List("ns-v1") {
		got = append(got, e.Meta.Coordinate+"@"+e.Meta.Target)
	}
	want := "kn.platform:Foundation:2.4.21@ios_arm64 kn.platform:UIKit:2.4.21@ios_arm64 kn.platform:UIKit:2.4.21@ios_simulator_arm64"
	if strings.Join(got, " ") != want {
		t.Errorf("List() = %v, want %s", got, want)
	}

	size, err := store.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if size < 30 {
		t.Errorf("Size() = %d, want at least the 30 bytes of source files", size)
	}
}

func TestCheck(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	readOnly := filepath.Join(base, "read-only")
	existing := filepath.Join(base, "existing")
	for _, dir := range []string{readOnly, existing} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o755) })

	tests := []struct {
		name string
		root string
		want string // part of the message; empty means usable
		// The check looks at permissions, which root does not have.
		needsUser bool
	}{
		{"an existing directory", existing, "", false},
		{"a directory that does not exist yet", filepath.Join(base, "new", "deeper"), "", false},
		{"a file", file, "a file is in the way at " + file, false},
		{"below a file", filepath.Join(file, "cache"), "a file is in the way at " + file, false},
		{"a read-only directory", readOnly, readOnly + " is not writable", true},
		{"below a read-only directory", filepath.Join(readOnly, "sub", "cache"), readOnly + " is not writable", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needsUser && os.Geteuid() == 0 {
				t.Skip("root can write everywhere")
			}
			err := Store{Root: tt.root}.Check()
			if tt.want == "" {
				if err != nil {
					t.Errorf("Check() error = %v, want nil", err)
				}
			} else if errs.Code(err) != errs.Cache || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), EnvDir) {
				t.Errorf("Check() error = %v, want %s with %q and a hint that names %s", err, errs.Cache, tt.want, EnvDir)
			}
		})
	}
	// The check creates nothing.
	if _, err := os.Stat(filepath.Join(base, "new")); !os.IsNotExist(err) {
		t.Errorf("Check() created %s (stat error = %v), want it to create nothing", filepath.Join(base, "new"), err)
	}
}

// A build has its meta.json before it is renamed into place. Until then its
// directory is not an entry: a search that listed it would lose it mid-run.
func TestListSkipsTheDirectoryOfABuildInProgress(t *testing.T) {
	store := Store{Root: t.TempDir()}
	if _, err := store.Ensure("ns-v1", "key", writeOne("A.kt", "a\n")); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(store.Root, "ns-v1", ".tmp-123456")
	if err := os.MkdirAll(filepath.Join(tmp, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), []byte("{\"coordinate\": \"kn.platform:Half:2.4.21\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := store.List("ns-v1")
	if len(entries) != 1 || entries[0].Meta.Coordinate != "kn.platform:UIKit:2.4.21" {
		t.Errorf("List() = %+v, want only the entry that is in place", entries)
	}
	if usage := store.Usage("ns-v1"); len(usage[0].Entries) != 1 {
		t.Errorf("Usage() lists %d entries, want 1", len(usage[0].Entries))
	}
}

func TestEnsureSweepsLeftoversOlderThanAnHour(t *testing.T) {
	store := Store{Root: t.TempDir()}
	ns := filepath.Join(store.Root, "ns-v1")
	old := filepath.Join(ns, ".tmp-old")
	fresh := filepath.Join(ns, ".tmp-fresh")
	foreign := filepath.Join(ns, "old-but-not-a-leftover")
	for _, dir := range []string{old, fresh, foreign} {
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "src", "A.kt"), []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	for _, dir := range []string{old, foreign} {
		if err := os.Chtimes(dir, twoHoursAgo, twoHoursAgo); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.Ensure("ns-v1", "key", writeOne("A.kt", "a\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the leftover of a build that died two hours ago is still there (stat error = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a temporary directory of this hour was removed, though its build may still run: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a directory that is not a temporary directory of the tool was removed: %v", err)
	}
}

func TestEnsureFromTwoGoroutinesLeavesOneEntry(t *testing.T) {
	for round := 0; round < 20; round++ {
		store := Store{Root: t.TempDir()}
		var builds atomic.Int32
		build := func(srcDir string) (Meta, error) {
			builds.Add(1)
			time.Sleep(20 * time.Millisecond) // long enough for the two builds to overlap
			return writeOne("A.kt", "a\n")(srcDir)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var entries [2]Entry
		var failures [2]error
		for i := range entries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				entries[i], failures[i] = store.Ensure("ns-v1", "key", build)
			}()
		}
		close(start)
		wg.Wait()

		for i, err := range failures {
			if err != nil {
				t.Fatalf("round %d: Ensure() of goroutine %d error = %v", round, i, err)
			}
			if data, err := os.ReadFile(filepath.Join(entries[i].Src, "A.kt")); err != nil || string(data) != "a\n" {
				t.Errorf("round %d: goroutine %d got an entry whose file reads %q, %v", round, i, data, err)
			}
		}
		if entries[0].Dir != entries[1].Dir {
			t.Errorf("round %d: the goroutines got two entries, %s and %s", round, entries[0].Dir, entries[1].Dir)
		}
		dirs, err := os.ReadDir(filepath.Join(store.Root, "ns-v1"))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, d := range dirs {
			names = append(names, d.Name())
		}
		if len(names) != 1 || names[0] != "key" {
			t.Errorf("round %d: the namespace holds %q after %d builds, want only the entry", round, names, builds.Load())
		}
	}
}

func TestDropRemovesOneEntryAndNothingElse(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "cache")}
	build := func(srcDir string) (Meta, error) {
		return Meta{Coordinate: "x:y:1", Files: 1}, os.WriteFile(filepath.Join(srcDir, "A.kt"), []byte("a\n"), 0o644)
	}
	first, err := store.Ensure("ns", "first", build)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Ensure("ns", "second", build)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Drop(first); err != nil {
		t.Fatalf("Drop(first) error = %v", err)
	}
	if _, ok := store.Get("ns", "first"); ok {
		t.Error("Get(first) after Drop finds the entry")
	}
	if _, ok := store.Get("ns", "second"); !ok {
		t.Error("Drop(first) removed the second entry")
	}
	left, err := os.ReadDir(filepath.Join(store.Root, "ns"))
	if err != nil || len(left) != 1 || left[0].Name() != "second" {
		t.Errorf("the namespace holds %v (%v) after Drop, want only the second entry", left, err)
	}
	if err := store.Drop(first); err != nil {
		t.Errorf("Drop() of an entry that is gone: error = %v, want nil", err)
	}

	// Only a directory two levels below the root is an entry of this store.
	outside := filepath.Join(t.TempDir(), "keep")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{
		"a directory outside the root": outside,
		"the namespace":                filepath.Join(store.Root, "ns"),
		"the root":                     store.Root,
		"a directory inside an entry":  second.Src,
		"a dot name":                   filepath.Join(store.Root, "ns", ".."),
	} {
		if err := store.Drop(Entry{Dir: dir}); err == nil {
			t.Errorf("Drop(%s) error = nil, want a refusal", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("Drop() removed a directory outside the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second.Src, "A.kt")); err != nil {
		t.Errorf("Drop() damaged the second entry: %v", err)
	}
}
