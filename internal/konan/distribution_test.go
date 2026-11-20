package konan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func env(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

// projectWithKotlin creates a project directory whose version catalog names
// the given Kotlin version.
func projectWithKotlin(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "gradle", "libs.versions.toml"),
		[]byte("[versions]\nagp = \"9.0.0\"\nkotlin = \""+version+"\"\n\n[libraries]\nkotlin = { module = \"x:y\" }\n"), 0o644)
	return dir
}

func TestDiscoverOrder(t *testing.T) {
	userHome := t.TempDir()
	dataDir := filepath.Join(userHome, ".konan")
	old := testutil.NewDistribution(t, dataDir, "2.4.10")
	mid := testutil.NewDistribution(t, dataDir, "2.4.21")
	rc := testutil.NewDistribution(t, dataDir, "2.5.0-RC1")
	// Noise that must be ignored: a non-distribution directory and a
	// distribution-like name without a klib directory.
	if err := os.MkdirAll(filepath.Join(dataDir, "dependencies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "kotlin-native-prebuilt-macos-aarch64-9.9.9"), 0o755); err != nil {
		t.Fatal(err)
	}

	otherData := t.TempDir()
	other := testutil.NewDistribution(t, otherData, "2.3.0")
	project := projectWithKotlin(t, "2.4.10")

	tests := []struct {
		name string
		opts DiscoverOptions
		want testutil.Distribution
	}{
		{
			name: "konan-home flag wins over everything",
			opts: DiscoverOptions{KonanHome: other.Home, ProjectDir: project, Getenv: env(map[string]string{"KONAN_HOME": mid.Home})},
			want: other,
		},
		{
			name: "KONAN_HOME wins over the project and the newest",
			opts: DiscoverOptions{ProjectDir: project, Getenv: env(map[string]string{"KONAN_HOME": mid.Home})},
			want: mid,
		},
		{
			name: "kotlin-version wins over the project",
			opts: DiscoverOptions{KotlinVersion: "2.4.21", ProjectDir: project},
			want: mid,
		},
		{
			name: "project version catalog",
			opts: DiscoverOptions{ProjectDir: project},
			want: old,
		},
		{
			name: "newest installed without any hint",
			opts: DiscoverOptions{ProjectDir: t.TempDir()},
			want: rc,
		},
		{
			name: "KONAN_DATA_DIR replaces ~/.konan",
			opts: DiscoverOptions{Getenv: env(map[string]string{"KONAN_DATA_DIR": otherData})},
			want: other,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.UserHome = userHome
			got, err := Discover(tt.opts)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if got.Home != tt.want.Home || got.Version != tt.want.Version {
				t.Errorf("Discover() = %+v, want home %s version %s", got, tt.want.Home, tt.want.Version)
			}
		})
	}
}

func TestDiscoverFallsBackWhenTheProjectVersionIsNotInstalled(t *testing.T) {
	userHome := t.TempDir()
	newest := testutil.NewDistribution(t, filepath.Join(userHome, ".konan"), "2.4.21")
	testutil.NewDistribution(t, filepath.Join(userHome, ".konan"), "2.4.10")

	var warn bytes.Buffer
	got, err := Discover(DiscoverOptions{ProjectDir: projectWithKotlin(t, "2.6.0"), UserHome: userHome, Warn: &warn})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got.Home != newest.Home {
		t.Errorf("Discover() home = %s, want the newest %s", got.Home, newest.Home)
	}
	if !strings.Contains(warn.String(), "2.6.0") || !strings.Contains(warn.String(), "using 2.4.21") {
		t.Errorf("warning = %q, want it to name both versions", warn.String())
	}
}

func TestDiscoverErrors(t *testing.T) {
	userHome := t.TempDir()
	testutil.NewDistribution(t, filepath.Join(userHome, ".konan"), "2.4.21")
	emptyHome := t.TempDir()
	notADist := t.TempDir()

	tests := []struct {
		name     string
		opts     DiscoverOptions
		contains []string
	}{
		{
			name:     "nothing installed",
			opts:     DiscoverOptions{UserHome: emptyHome},
			contains: []string{"no Kotlin/Native distribution found", "Try: run a Kotlin/Native Gradle task once"},
		},
		{
			name:     "requested version is not installed",
			opts:     DiscoverOptions{KotlinVersion: "2.0.0", UserHome: userHome},
			contains: []string{"no Kotlin/Native 2.0.0 distribution", "installed: 2.4.21"},
		},
		{
			name:     "konan-home is not a distribution",
			opts:     DiscoverOptions{KonanHome: notADist, UserHome: userHome},
			contains: []string{"--konan-home points at", "not a Kotlin/Native distribution"},
		},
		{
			name:     "KONAN_HOME is not a distribution",
			opts:     DiscoverOptions{Getenv: env(map[string]string{"KONAN_HOME": notADist}), UserHome: userHome},
			contains: []string{"KONAN_HOME points at"},
		},
		{
			name: "explicit home disagrees with the requested version",
			opts: DiscoverOptions{
				KonanHome:     filepath.Join(userHome, ".konan", "kotlin-native-prebuilt-macos-aarch64-2.4.21"),
				KotlinVersion: "2.4.10",
				UserHome:      userHome,
			},
			contains: []string{"Kotlin 2.4.10 was requested", "2.4.21 distribution"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Discover(tt.opts)
			if errs.Code(err) != errs.NoKonan {
				t.Fatalf("Discover() error = %v, want %s", err, errs.NoKonan)
			}
			for _, part := range tt.contains {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q does not contain %q", err.Error(), part)
				}
			}
		})
	}
}

func TestExplicitHomeVersionFromManifest(t *testing.T) {
	// A distribution at a path whose name carries no version.
	home := filepath.Join(t.TempDir(), "my-konan")
	manifest := filepath.Join(home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.darwin", "default", "manifest")
	testutil.WriteFile(t, manifest, []byte("compiler_version=2.4.21\n"), 0o644)

	got, err := Discover(DiscoverOptions{KonanHome: home})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got.Version != "2.4.21" {
		t.Errorf("Version = %q, want 2.4.21 from the manifest", got.Version)
	}

	bare := filepath.Join(t.TempDir(), "bare")
	if err := os.MkdirAll(filepath.Join(bare, "klib"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = Discover(DiscoverOptions{KonanHome: bare})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got.Version != UnknownVersion {
		t.Errorf("Version = %q, want %q", got.Version, UnknownVersion)
	}
}

func TestVersionFromDirName(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{"kotlin-native-prebuilt-macos-aarch64-2.4.21", "2.4.21", true},
		{"kotlin-native-prebuilt-linux-x86_64-2.5.0-RC2", "2.5.0-RC2", true},
		{"kotlin-native-macos-aarch64-1.9.24", "1.9.24", true},
		{"dependencies", "", false},
		{"kotlin-native-prebuilt-macos-aarch64", "", false},
		{"kotlin-native-prebuilt-macos-aarch64-", "", false},
	}
	for _, tt := range tests {
		got, ok := versionFromDirName(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("versionFromDirName(%q) = %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
