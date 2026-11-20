package xcode

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

type fixture struct {
	xcode  *testutil.Xcode
	runner *testutil.Overlay
	ios    string // real directory of the iPhoneOS SDK
	mac    string // real directory of the MacOSX SDK
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	x := testutil.NewXcode(t)
	ios := x.AddSDK(t, "iPhoneOS", "26.4")
	x.AddHeader(t, ios, "UIKit", "UIViewController.h", "@interface UIViewController\n@end\n")
	x.AddHeader(t, ios, "Foundation", "NSURL.h", "@interface NSURL\n@end\n")
	x.AddHeader(t, ios, "CoreFoundation", "CFBase.h", "typedef int CFIndex;\n")
	x.AddSwiftOnlyFramework(t, ios, "SwiftUI")
	mac := x.AddSDK(t, "MacOSX", "26.4")
	x.AddVersionedHeader(t, mac, "AppKit", "NSWindow.h", "@interface NSWindow\n@end\n")
	return fixture{
		xcode:  x,
		runner: &testutil.Overlay{Base: executil.OS{}, Fakes: x.Fakes()},
		ios:    ios,
		mac:    mac,
	}
}

func TestFind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, name := range []string{"", "iphoneos", "iphoneos26.4"} {
		sdk, err := Find(ctx, f.runner, name)
		if err != nil {
			t.Fatalf("Find(%q) error = %v", name, err)
		}
		// xcrun answers with the versioned symbolic link; the canonical name
		// comes from SDKSettings.json.
		wantPath := filepath.Join(filepath.Dir(f.ios), "iPhoneOS26.4.sdk")
		if sdk.Name != "iphoneos26.4" || sdk.Version != "26.4" || sdk.Path != wantPath {
			t.Errorf("Find(%q) = %+v, want iphoneos26.4 at %s", name, sdk, wantPath)
		}
	}

	sdk, err := Find(ctx, f.runner, "macosx")
	if err != nil || sdk.Name != "macosx26.4" {
		t.Errorf("Find(macosx) = %+v, %v", sdk, err)
	}
}

func TestFindWithoutSDKSettings(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.ios, "SDKSettings.json")); err != nil {
		t.Fatal(err)
	}
	sdk, err := Find(context.Background(), f.runner, "iphoneos")
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if sdk.Name != "iphoneos" || sdk.Version != "" {
		t.Errorf("Find() = %+v, want the requested name as a fallback", sdk)
	}
}

func TestFindErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := Find(ctx, f.runner, "nosuchsdk")
	if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), `the Apple SDK "nosuchsdk" is not installed`) || !strings.Contains(err.Error(), "ksrc-native deps") {
		t.Errorf("Find(unknown SDK) error = %v, want %s with a deps hint", err, errs.NoSources)
	}

	// No xcrun at all.
	_, err = Find(ctx, &testutil.Runner{}, "iphoneos")
	if errs.Code(err) != errs.NoXcode || !strings.Contains(err.Error(), "xcrun is not on PATH") || !strings.Contains(err.Error(), "xcode-select -s") {
		t.Errorf("Find(no xcrun) error = %v, want %s with an install hint", err, errs.NoXcode)
	}

	// xcrun exists, but no developer tools are selected.
	broken := &testutil.Runner{
		Paths: map[string]string{"xcrun": "/usr/bin/xcrun"},
		RunFunc: func(context.Context, executil.Command) error {
			return &executil.ExitError{Path: "xcrun", Code: 1, Stderr: "xcrun: error: invalid active developer path (/Library/Developer/CommandLineTools), missing xcrun\n"}
		},
	}
	_, err = Find(ctx, broken, "iphoneos")
	if errs.Code(err) != errs.NoXcode || !strings.Contains(err.Error(), "invalid active developer path") {
		t.Errorf("Find(broken xcrun) error = %v, want %s with xcrun's message", err, errs.NoXcode)
	}

	// xcrun prints a path that does not exist.
	ghost := &testutil.Runner{
		Paths: map[string]string{"xcrun": "/usr/bin/xcrun"},
		RunFunc: func(_ context.Context, cmd executil.Command) error {
			_, err := cmd.Stdout.Write([]byte("/no/such/sdk\n"))
			return err
		},
	}
	if _, err = Find(ctx, ghost, "iphoneos"); errs.Code(err) != errs.NoXcode {
		t.Errorf("Find(ghost path) error = %v, want %s", err, errs.NoXcode)
	}
}

func TestFrameworksAndFindHeaders(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sdk, err := Find(ctx, f.runner, "iphoneos")
	if err != nil {
		t.Fatal(err)
	}

	// Swift-only frameworks are not listed.
	if got, want := strings.Join(sdk.Frameworks(), " "), "CoreFoundation Foundation UIKit"; got != want {
		t.Errorf("Frameworks() = %q, want %q", got, want)
	}

	headers, err := sdk.FindHeaders("uikit")
	if err != nil {
		t.Fatalf("FindHeaders() error = %v", err)
	}
	wantDir := filepath.Join(sdk.Path, "System", "Library", "Frameworks", "UIKit.framework", "Headers")
	if headers.Framework != "UIKit" || headers.Dir != wantDir {
		t.Errorf("FindHeaders() = %+v, want UIKit at %s", headers, wantDir)
	}

	tests := []struct {
		name, framework, part string
	}{
		{"swift-only framework", "SwiftUI", "has no Objective-C headers"},
		{"near miss", "Foundatio", "did you mean CoreFoundation, Foundation?"},
		{"unrelated name", "Retrofit", "3 have headers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sdk.FindHeaders(tt.framework)
			if errs.Code(err) != errs.NoSources || !strings.Contains(err.Error(), tt.part) {
				t.Errorf("FindHeaders(%q) error = %v, want %s with %q", tt.framework, err, errs.NoSources, tt.part)
			}
		})
	}

	// macOS keeps Headers behind a symbolic link.
	mac, err := Find(ctx, f.runner, "macosx")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(mac.Frameworks(), " "); got != "AppKit" {
		t.Errorf("macOS Frameworks() = %q, want AppKit", got)
	}
	if _, err := mac.FindHeaders("AppKit"); err != nil {
		t.Errorf("FindHeaders(AppKit) error = %v", err)
	}

	// An SDK directory without frameworks.
	if _, err := (SDK{Name: "empty1.0", Path: t.TempDir()}).FindHeaders("UIKit"); errs.Code(err) != errs.NoSources {
		t.Errorf("FindHeaders on an empty SDK error = %v, want %s", err, errs.NoSources)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	sdks, err := List(context.Background(), f.runner)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	// Each SDK is listed once, although it is reachable under two names.
	var names []string
	for _, s := range sdks {
		names = append(names, s.Name)
	}
	if got, want := strings.Join(names, " "), "iphoneos26.4 macosx26.4"; got != want {
		t.Errorf("List() = %q, want %q", got, want)
	}

	// The Command Line Tools keep SDKs directly below the developer directory.
	clt := filepath.Join(t.TempDir(), "CommandLineTools")
	testutil.WriteFile(t, filepath.Join(clt, "SDKs", "MacOSX15.4.sdk", "SDKSettings.json"), []byte(`{"CanonicalName":"macosx15.4","Version":"15.4"}`), 0o644)
	if err := os.MkdirAll(filepath.Join(clt, "SDKs", "MacOSX99.sdk"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &testutil.Runner{
		Paths: map[string]string{"xcode-select": "/usr/bin/xcode-select"},
		RunFunc: func(_ context.Context, cmd executil.Command) error {
			_, err := cmd.Stdout.Write([]byte(clt + "\n"))
			return err
		},
	}
	sdks, err = List(context.Background(), runner)
	if err != nil || len(sdks) != 2 || sdks[0].Name != "macosx15.4" || sdks[1].Name != "macosx99" {
		t.Errorf("List(Command Line Tools) = %+v, %v; want macosx15.4 and the fallback name macosx99", sdks, err)
	}

	if _, err := List(context.Background(), &testutil.Runner{}); errs.Code(err) != errs.NoXcode {
		t.Errorf("List(no xcode-select) error = %v, want %s", err, errs.NoXcode)
	}
	failing := &testutil.Runner{
		Paths: map[string]string{"xcode-select": "/usr/bin/xcode-select"},
		RunFunc: func(context.Context, executil.Command) error {
			return &executil.ExitError{Path: "xcode-select", Code: 2, Stderr: "no developer tools"}
		},
	}
	if _, err := List(context.Background(), failing); errs.Code(err) != errs.NoXcode {
		t.Errorf("List(failing xcode-select) error = %v, want %s", err, errs.NoXcode)
	}
}

func TestValidSDKName(t *testing.T) {
	for _, name := range []string{"iphoneos", "iphonesimulator", "macosx", "iphoneos26.4", "macosx26.4", "watchos", "xros2.0", "MacOSX"} {
		if !ValidSDKName(name) {
			t.Errorf("ValidSDKName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "/tmp", "/Applications/Xcode.app/x.sdk", "../sdk", "..", ".", "sdk/x", "iphone os", "-iphoneos", "--show-sdk-version", "iphoneos;ls", "iphoneos\n", "26.4", "iphoneos_26", "iphoneos-26"} {
		if ValidSDKName(name) {
			t.Errorf("ValidSDKName(%q) = true, want false", name)
		}
	}
}

// xcrun answers `--sdk <directory>` with that directory, so Find must not
// ask it about anything that is not a name.
func TestFindRefusesAPathWithoutRunningXcrun(t *testing.T) {
	dir := t.TempDir()
	runner := &testutil.Runner{
		Paths: map[string]string{"xcrun": "/usr/bin/xcrun"},
		RunFunc: func(_ context.Context, cmd executil.Command) error {
			_, err := io.WriteString(cmd.Stdout, dir+"\n")
			return err
		},
	}
	for _, name := range []string{dir, "/tmp", "../sdk", "-iphoneos"} {
		sdk, err := Find(context.Background(), runner, name)
		if errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), "is not an Apple SDK name") || !strings.Contains(err.Error(), "Try: use iphoneos, iphonesimulator, macosx") {
			t.Errorf("Find(%q) = %+v, %v; want %s", name, sdk, err, errs.BadArgs)
		}
	}
	if calls := runner.Calls(); len(calls) != 0 {
		t.Errorf("Find ran xcrun %d times for values that are not names, want 0", len(calls))
	}
}

// xcrun or xcode-select that the tool stopped says nothing about Xcode.
func TestAStoppedRunIsNotAMissingXcode(t *testing.T) {
	stop := errs.New(errs.Timeout, "the program did not finish within 15s and was stopped", "run it in a terminal")
	runner := &testutil.Runner{
		Paths:   map[string]string{"xcrun": "/usr/bin/xcrun", "xcode-select": "/usr/bin/xcode-select"},
		RunFunc: func(context.Context, executil.Command) error { return stop },
	}
	if _, err := Find(context.Background(), runner, "iphoneos"); err != error(stop) {
		t.Errorf("Find() with a stopped xcrun: error = %v, want the stop error unchanged", err)
	}
	if _, err := List(context.Background(), runner); err != error(stop) {
		t.Errorf("List() with a stopped xcode-select: error = %v, want the stop error unchanged", err)
	}
}
