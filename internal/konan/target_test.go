package konan

import (
	"path/filepath"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func TestHostTarget(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"kotlin-native-prebuilt-linux-x86_64-2.4.21", "linux_x64"},
		{"kotlin-native-prebuilt-macos-aarch64-2.4.21", "macos_arm64"},
		{"kotlin-native-prebuilt-macos-x86_64-2.4.21", "macos_x64"},
		{"kotlin-native-prebuilt-windows-x86_64-2.4.21", "mingw_x64"},
		{"kotlin-native-linux-x86_64-1.9.24", "linux_x64"},
		{"kotlin-native-prebuilt-linux-x86_64-2.5.0-RC1", "linux_x64"},
		{"my-konan", ""},
		{"linux-x86_64", ""},
	}
	for _, tt := range tests {
		d := Distribution{Home: filepath.Join("/data", tt.dir)}
		if got := d.HostTarget(); got != tt.want {
			t.Errorf("Distribution at %s: HostTarget() = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestSystemTarget(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "linux_x64"},
		{"linux", "arm64", "linux_arm64"},
		{"darwin", "arm64", "macos_arm64"},
		{"darwin", "amd64", "macos_x64"},
		{"windows", "amd64", "mingw_x64"},
		{"linux", "riscv64", ""},
		{"freebsd", "amd64", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := SystemTarget(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("SystemTarget(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestPickTarget(t *testing.T) {
	dist := func(host string, targets ...string) Distribution {
		fake := testutil.NewDistributionFor(t, t.TempDir(), host, "2.4.21")
		for _, target := range targets {
			fake.AddPlatformKlib(t, target, "posix", nil)
		}
		return Distribution{Home: fake.Home, Version: fake.Version}
	}
	// A distribution at a path that does not name its host.
	plain := Distribution{Home: filepath.Join(t.TempDir(), "my-konan"), Version: "2.4.21"}
	testutil.WriteFile(t, filepath.Join(plain.Home, "klib", "platform", "linux_arm64", platformKlibPrefix+"posix", "default", "manifest"), []byte("compiler_version=2.4.21\n"), 0o644)

	tests := []struct {
		name         string
		dist         Distribution
		goos, goarch string
		want         string
	}{
		{"ios_arm64 when the distribution has it", dist("macos-aarch64", "ios_arm64", "macos_arm64"), "darwin", "arm64", "ios_arm64"},
		{"ios_arm64 wins over the host on any system", dist("macos-aarch64", "ios_arm64", "macos_arm64"), "linux", "amd64", "ios_arm64"},
		{"the host of a Linux distribution", dist("linux-x86_64", "linux_x64", "linux_arm64", "android_arm64"), "linux", "amd64", "linux_x64"},
		{"the directory name wins over the system", dist("linux-x86_64", "linux_x64", "linux_arm64"), "linux", "arm64", "linux_x64"},
		{"the system when the directory does not name a host", plain, "linux", "arm64", "linux_arm64"},
		{"the system when the host of the name has no libraries", dist("linux-x86_64", "linux_arm64"), "linux", "arm64", "linux_arm64"},
		{"ios_arm64 when nothing fits, so the error names it", dist("linux-x86_64", "android_arm64"), "linux", "amd64", "ios_arm64"},
		{"ios_arm64 for an unknown system", dist("linux-x86_64", "android_arm64"), "", "", "ios_arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dist.PickTarget(tt.goos, tt.goarch); got != tt.want {
				t.Errorf("PickTarget(%q, %q) = %q, want %q (targets: %v)", tt.goos, tt.goarch, got, tt.want, tt.dist.Targets())
			}
		})
	}
}
