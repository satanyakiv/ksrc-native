package konan

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func fakeDistribution(t *testing.T) (Distribution, testutil.Distribution) {
	t.Helper()
	fake := testutil.NewDistribution(t, t.TempDir(), "2.4.21")
	fake.AddPlatformKlib(t, "ios_arm64", "UIKit", []byte("library {\n}\n"))
	fake.AddPlatformKlib(t, "ios_arm64", "Foundation", nil)
	fake.AddPlatformKlib(t, "ios_arm64", "CoreFoundation", nil)
	fake.AddPlatformKlib(t, "macos_arm64", "AppKit", nil)
	return Distribution{Home: fake.Home, Version: fake.Version}, fake
}

func TestTargetsAndFrameworks(t *testing.T) {
	dist, _ := fakeDistribution(t)

	if got, want := strings.Join(dist.Targets(), " "), "ios_arm64 macos_arm64"; got != want {
		t.Errorf("Targets() = %q, want %q", got, want)
	}
	if got, want := strings.Join(dist.Frameworks("ios_arm64"), " "), "CoreFoundation Foundation UIKit"; got != want {
		t.Errorf("Frameworks(ios_arm64) = %q, want %q", got, want)
	}
	if got := dist.Frameworks("tvos_arm64"); len(got) != 0 {
		t.Errorf("Frameworks(tvos_arm64) = %v, want none", got)
	}
}

func TestFindPlatformKlib(t *testing.T) {
	dist, fake := fakeDistribution(t)

	got, err := dist.FindPlatformKlib("ios_arm64", "uikit")
	if err != nil {
		t.Fatalf("FindPlatformKlib() error = %v", err)
	}
	wantPath := filepath.Join(fake.Home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.UIKit")
	if got.Framework != "UIKit" || got.Target != "ios_arm64" || got.Path != wantPath {
		t.Errorf("FindPlatformKlib() = %+v, want UIKit at %s", got, wantPath)
	}

	tests := []struct {
		name, target, framework string
		contains                []string
	}{
		{"near miss suggests names", "ios_arm64", "Foundatio", []string{`no platform library "Foundatio"`, "did you mean CoreFoundation, Foundation?"}},
		{"unrelated name explains the naming", "ios_arm64", "Retrofit", []string{"import platform.<Framework>", "3 are available"}},
		{"unknown target lists targets", "watchos_arm64", "UIKit", []string{`target "watchos_arm64"`, "ios_arm64, macos_arm64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dist.FindPlatformKlib(tt.target, tt.framework)
			if errs.Code(err) != errs.NoSources {
				t.Fatalf("error = %v, want %s", err, errs.NoSources)
			}
			for _, part := range tt.contains {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q does not contain %q", err.Error(), part)
				}
			}
		})
	}
}

func TestManifest(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "default", "manifest"),
		[]byte("# comment\nabi_version=2.4.0\ncompiler_version = 2.4.21\ncompilerOpts=-framework UIKit\nexportForwardDeclarations=\nno-equals-line\nlast=value"), 0o644)

	got, err := Manifest(dir)
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	want := map[string]string{
		"abi_version":               "2.4.0",
		"compiler_version":          "2.4.21",
		"compilerOpts":              "-framework UIKit",
		"exportForwardDeclarations": "",
		"last":                      "value",
	}
	if len(got) != len(want) {
		t.Errorf("Manifest() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Manifest()[%q] = %q, want %q", k, got[k], v)
		}
	}

	if _, err := Manifest(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Manifest(no manifest) error = %v, want not-exist", err)
	}
}

func TestDumpMetadataRunsTheKlibTool(t *testing.T) {
	dist, fake := fakeDistribution(t)
	klib, err := dist.FindPlatformKlib("ios_arm64", "UIKit")
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := dist.DumpMetadata(context.Background(), executil.OS{}, klib.Path, &out); err != nil {
		t.Fatalf("DumpMetadata() error = %v", err)
	}
	if out.String() != "library {\n}\n" {
		t.Errorf("dump = %q, want the fixture text", out.String())
	}
	calls := fake.KlibCalls(t)
	if len(calls) != 1 || calls[0] != "dump-metadata "+klib.Path {
		t.Errorf("klib calls = %q, want one dump-metadata call", calls)
	}
}

func TestDumpMetadataErrors(t *testing.T) {
	dist, _ := fakeDistribution(t)

	// The fake tool fails for a library without a dump fixture.
	klib, err := dist.FindPlatformKlib("ios_arm64", "Foundation")
	if err != nil {
		t.Fatal(err)
	}
	err = dist.DumpMetadata(context.Background(), executil.OS{}, klib.Path, &bytes.Buffer{})
	if errs.Code(err) != errs.KlibFailed {
		t.Fatalf("error = %v, want %s", err, errs.KlibFailed)
	}
	if !strings.Contains(err.Error(), "library not found") || !strings.Contains(err.Error(), "JDK") {
		t.Errorf("error %q should carry the tool's stderr and the JDK hint", err.Error())
	}

	// A distribution without the tool.
	if err := os.Remove(dist.KlibTool()); err != nil {
		t.Fatal(err)
	}
	runner := &testutil.Runner{}
	err = dist.DumpMetadata(context.Background(), runner, klib.Path, &bytes.Buffer{})
	if errs.Code(err) != errs.NoKonan {
		t.Errorf("error = %v, want %s", err, errs.NoKonan)
	}
	if len(runner.Calls()) != 0 {
		t.Error("the runner was called although the tool is missing")
	}
}

// A target is compared as a string with the names in klib/platform. A value
// that only reaches the same directory through the file system is refused.
func TestRequireTargetWantsTheExactName(t *testing.T) {
	dist, fake := fakeDistribution(t)
	// A directory without libraries is not a target.
	if err := os.MkdirAll(filepath.Join(fake.Home, "klib", "platform", "tvos_arm64"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"ios_arm64", "macos_arm64"} {
		if err := dist.RequireTarget(target); err != nil {
			t.Errorf("RequireTarget(%q) error = %v, want nil", target, err)
		}
	}
	for _, target := range []string{
		"", "tvos_arm64", "watchos_arm64",
		"IOS_ARM64", "Ios_Arm64",
		"../platform/ios_arm64", "ios_arm64/", "./ios_arm64", "ios_arm64/../macos_arm64", "ios_arm64/.",
		" ios_arm64", "ios_arm64 ",
	} {
		err := dist.RequireTarget(target)
		if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), "pass --target with one of: ios_arm64, macos_arm64") {
			t.Errorf("RequireTarget(%q) error = %v, want %s with the target list", target, err, errs.NoSources)
		}
		if _, err := dist.FindPlatformKlib(target, "UIKit"); errs.Code(err) != errs.NoSources {
			t.Errorf("FindPlatformKlib(%q, UIKit) error = %v, want %s", target, err, errs.NoSources)
		}
	}
}
