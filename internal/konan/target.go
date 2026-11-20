package konan

import (
	"path/filepath"
	"strings"
)

// hostTargets maps the host part of a distribution directory name, as in
// kotlin-native-prebuilt-linux-x86_64-2.4.21, to the target of that host.
var hostTargets = []struct{ host, target string }{
	{"linux-x86_64", "linux_x64"},
	{"macos-aarch64", "macos_arm64"},
	{"macos-x86_64", "macos_x64"},
	{"windows-x86_64", "mingw_x64"},
	{"mingw-x86_64", "mingw_x64"},
}

// HostTarget returns the target of the system the distribution runs on, read
// from the name of its directory, or "" when the name does not say.
func (d Distribution) HostTarget() string {
	name := filepath.Base(d.Home)
	if !strings.HasPrefix(name, "kotlin-native-") {
		return ""
	}
	for _, h := range hostTargets {
		if strings.Contains(name, "-"+h.host+"-") {
			return h.target
		}
	}
	return ""
}

// SystemTarget returns the Kotlin/Native target of a Go system pair
// (runtime.GOOS, runtime.GOARCH), or "" when Kotlin/Native has no such
// target.
func SystemTarget(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux_x64"
	case "linux/arm64":
		return "linux_arm64"
	case "darwin/arm64":
		return "macos_arm64"
	case "darwin/amd64":
		return "macos_x64"
	case "windows/amd64":
		return "mingw_x64"
	}
	return ""
}

// PickTarget returns the target to use when the caller named none:
// DefaultTarget when the distribution has its platform libraries, else the
// host target, first by the name of the distribution directory and then by
// the system the tool runs on. A distribution for Linux or Windows has no
// Apple targets, so ios_arm64 would fail every command there.
//
// When nothing fits it returns DefaultTarget, and the caller's check of the
// target fails with a message that names it and lists what is there.
func (d Distribution) PickTarget(goos, goarch string) string {
	for _, target := range []string{DefaultTarget, d.HostTarget(), SystemTarget(goos, goarch)} {
		if target != "" && d.RequireTarget(target) == nil {
			return target
		}
	}
	return DefaultTarget
}
