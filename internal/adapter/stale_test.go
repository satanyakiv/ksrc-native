package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/provider"
)

// plantDump writes a platform entry as another version of the splitter would
// have left it. Its library is not the one the test searches, so nothing
// removes it.
func plantDump(t *testing.T, w *world, name, format string) {
	t.Helper()
	store, err := cache.Open(w.a.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Ensure(provider.PlatformNamespace, cache.Key(name, format), func(srcDir string) (cache.Meta, error) {
		err := os.WriteFile(filepath.Join(srcDir, "Old.kt"), []byte("old\n"), 0o644)
		return cache.Meta{
			Coordinate: "kn.platform:" + name + ":2.4.21",
			Target:     "ios_arm64",
			Origin:     "/elsewhere/klib/" + name,
			Files:      1,
			Format:     format,
		}, err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A dump of another format is dead weight until its library is used again or
// the cache is cleaned. clean and doctor say so, because nothing else does.
func TestCleanAndDoctorMarkADumpOfAnotherFormat(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	plantDump(t, w, "CoreLocation", "0")
	plantDump(t, w, "UIKit", "")

	w.reset()
	if err := w.a.Clean(ctx, CleanRequest{DryRun: true}); err != nil {
		t.Fatalf("Clean(dry run) error = %v", err)
	}
	const mark = ", another dump format: not read any more"
	for _, line := range strings.Split(strings.TrimSpace(w.stdout.String()), "\n") {
		switch {
		case strings.HasPrefix(line, "kn.platform:CoreLocation:2.4.21 (ios_arm64) "), strings.HasPrefix(line, "kn.platform:UIKit:2.4.21 (ios_arm64) "):
			if !strings.HasSuffix(line, mark) {
				t.Errorf("Clean(dry run) line = %q, want it to end with %q", line, mark)
			}
		case strings.Contains(line, mark):
			t.Errorf("Clean(dry run) line = %q, want no mark on an entry of the current format", line)
		}
	}
	if got := strings.Count(w.stdout.String(), mark); got != 2 {
		t.Errorf("Clean(dry run) marked %d entries, want 2:\n%s", got, w.stdout.String())
	}
	if !strings.Contains(w.stdout.String(), "total: 3 entries, ") {
		t.Errorf("Clean(dry run) stdout lacks the total of 3 entries:\n%s", w.stdout.String())
	}

	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if line, want := doctorLine(t, w.stdout.String(), "cache"), " (3 platform dumps, 2 of another dump format, "; !strings.Contains(line, want) {
		t.Errorf("Doctor() cache line = %q, want it to hold %q", line, want)
	}
}

func TestDoctorSaysNothingAboutFormatsWhenAllDumpsAreCurrent(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if line := doctorLine(t, w.stdout.String(), "cache"); strings.Contains(line, "format") {
		t.Errorf("Doctor() cache line = %q, want no word about formats", line)
	}
}
