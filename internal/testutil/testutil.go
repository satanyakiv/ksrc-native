// Package testutil holds helpers shared by the tests of several packages: a
// fake Kotlin/Native distribution with a fake klib tool, a scripted Runner
// and access to the fixtures under the repository's testdata directory.
package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// RepoRoot returns the root directory of the repository.
func RepoRoot(tb testing.TB) string {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("testutil: cannot locate the source file")
	}
	// file is <root>/internal/testutil/testutil.go
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// FixturePath returns the path of a file under <root>/testdata.
func FixturePath(tb testing.TB, rel string) string {
	tb.Helper()
	return filepath.Join(RepoRoot(tb), "testdata", filepath.FromSlash(rel))
}

// ReadFixture reads a file under <root>/testdata.
func ReadFixture(tb testing.TB, rel string) []byte {
	tb.Helper()
	data, err := os.ReadFile(FixturePath(tb, rel))
	if err != nil {
		tb.Fatalf("testutil: read fixture: %v", err)
	}
	return data
}

// UserNotificationsDump is the fixture with a short real excerpt of
// `klib dump-metadata` output for platform.UserNotifications.
const UserNotificationsDump = "dumps/usernotifications.txt"

// WriteFile writes a file and creates its parent directories.
func WriteFile(tb testing.TB, path string, content []byte, mode os.FileMode) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
}

// Distribution is a fake Kotlin/Native distribution in a temporary directory.
type Distribution struct {
	DataDir string // stands in for ~/.konan
	Home    string // the distribution directory
	Version string
}

// NewDistribution creates a fake distribution of the given Kotlin version in
// dataDir. Its bin/klib is the fake tool from testdata/fake-klib.
func NewDistribution(tb testing.TB, dataDir, version string) Distribution {
	tb.Helper()
	home := filepath.Join(dataDir, "kotlin-native-prebuilt-macos-aarch64-"+version)
	if err := os.MkdirAll(filepath.Join(home, "klib", "platform"), 0o755); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	WriteFile(tb, filepath.Join(home, "bin", "klib"), ReadFixture(tb, "fake-klib/klib"), 0o755)
	return Distribution{DataDir: dataDir, Home: home, Version: version}
}

// AddPlatformKlib adds a platform library whose metadata dump is the given
// text and returns the klib directory.
func (d Distribution) AddPlatformKlib(tb testing.TB, target, framework string, dump []byte) string {
	tb.Helper()
	klib := filepath.Join(d.Home, "klib", "platform", target, "org.jetbrains.kotlin.native.platform."+framework)
	manifest := fmt.Sprintf("abi_version=2.4.0\ncompiler_version=%s\nnative_targets=%s\npackage=platform.%s\nunique_name=org.jetbrains.kotlin.native.platform.%s\n",
		d.Version, target, framework, framework)
	WriteFile(tb, filepath.Join(klib, "default", "manifest"), []byte(manifest), 0o644)
	if dump != nil {
		WriteFile(tb, filepath.Join(klib, "dump-metadata.txt"), dump, 0o644)
	}
	return klib
}

// KlibCalls returns the argument lines of all runs of the fake klib tool.
func (d Distribution) KlibCalls(tb testing.TB) []string {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join(d.Home, "klib-calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// Runner is a scripted executil.Runner.
type Runner struct {
	// Paths maps a program name to the path LookPath returns. A missing name
	// is "not found".
	Paths map[string]string
	// RunFunc handles Run. Nil makes every run succeed without output.
	RunFunc func(ctx context.Context, cmd executil.Command) error

	mu    sync.Mutex
	calls []executil.Command
}

// LookPath implements executil.Runner.
func (r *Runner) LookPath(name string) (string, error) {
	if path, ok := r.Paths[name]; ok {
		return path, nil
	}
	return "", fmt.Errorf("%s: executable file not found in $PATH", name)
}

// Run implements executil.Runner.
func (r *Runner) Run(ctx context.Context, cmd executil.Command) error {
	r.mu.Lock()
	r.calls = append(r.calls, cmd)
	r.mu.Unlock()
	if r.RunFunc == nil {
		return nil
	}
	return r.RunFunc(ctx, cmd)
}

// Calls returns the commands run so far.
func (r *Runner) Calls() []executil.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]executil.Command(nil), r.calls...)
}

// RequireRipgrep skips the test when rg is not installed.
func RequireRipgrep(tb testing.TB) {
	tb.Helper()
	if _, err := (executil.OS{}).LookPath("rg"); err != nil {
		tb.Skip("rg is not installed")
	}
}
