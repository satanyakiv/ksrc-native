package konan

import (
	"path/filepath"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.4.21", "2.4.21", 0},
		{"2.4.21", "2.4.20", 1},
		{"2.4.9", "2.4.10", -1},
		{"2.10.0", "2.9.9", 1},
		{"3.0.0", "2.99.99", 1},
		{"2.5", "2.5.0", 0},
		{"2.5.0", "2.5.0-RC2", 1},
		{"2.5.0-Beta1", "2.5.0", -1},
		{"2.5.0-RC1", "2.5.0-RC2", -1},
		{"2.5.0-RC1", "2.4.21", 1},
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		}
		return 0
	}
	for _, tt := range tests {
		if got := sign(CompareVersions(tt.a, tt.b)); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := sign(CompareVersions(tt.b, tt.a)); got != -tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
}

func TestProjectKotlinVersion(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
		ok   bool
	}{
		{"plain entry", "[versions]\nkotlin = \"2.4.21\"\n", "2.4.21", true},
		{"single quotes and a comment", "[versions]\nkotlin = '2.4.20' # pinned\n", "2.4.20", true},
		{"no spaces", "[versions]\nkotlin=\"2.4.10\"\n", "2.4.10", true},
		{"other keys around", "# catalog\n[versions]\nkotlinx-coroutines = \"1.10.0\"\nkotlin = \"2.4.21\"\nksp = \"2.4.21-1.0\"\n", "2.4.21", true},
		{"entry outside the versions table", "[libraries]\nkotlin = \"2.4.21\"\n", "", false},
		{"table form is not understood", "[versions]\nkotlin = { strictly = \"2.4.21\" }\n", "", false},
		{"no kotlin entry", "[versions]\nagp = \"9.0.0\"\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			testutil.WriteFile(t, filepath.Join(dir, "gradle", "libs.versions.toml"), []byte(tt.toml), 0o644)
			got, ok := ProjectKotlinVersion(dir)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ProjectKotlinVersion() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}

	if _, ok := ProjectKotlinVersion(t.TempDir()); ok {
		t.Error("ProjectKotlinVersion() found a version in a directory without a catalog")
	}
}
