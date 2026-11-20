package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func newApple(t *testing.T) (*AppleSDK, *testutil.Xcode) {
	t.Helper()
	x := testutil.NewXcode(t)
	ios := x.AddSDK(t, "iPhoneOS", "26.4")
	x.AddHeader(t, ios, "UIKit", "UIViewController.h", "@interface UIViewController\n- (void)presentViewController:(UIViewController *)vc;\n@end\n")
	x.AddSwiftOnlyFramework(t, ios, "SwiftUI")
	sim := x.AddSDK(t, "iPhoneSimulator", "26.4")
	x.AddHeader(t, sim, "UIKit", "UIViewController.h", "// simulator\n")
	mac := x.AddSDK(t, "MacOSX", "26.4")
	x.AddVersionedHeader(t, mac, "AppKit", "NSWindow.h", "@interface NSWindow\n@end\n")
	return &AppleSDK{Runner: &testutil.Overlay{Base: executil.OS{}, Fakes: x.Fakes()}}, x
}

func TestAppleResolve(t *testing.T) {
	a, _ := newApple(t)
	ctx := context.Background()

	// The default SDK, a case-insensitive framework name.
	src, err := a.Resolve(ctx, Request{Coordinate: coord(t, "sdk.apple:uikit")})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := src.Coordinate.String(); got != "sdk.apple:UIKit:iphoneos26.4" {
		t.Errorf("Coordinate = %s, want sdk.apple:UIKit:iphoneos26.4", got)
	}
	if got := src.FileID("UIViewController.h"); got != "sdk.apple:UIKit:iphoneos26.4!/UIViewController.h" {
		t.Errorf("FileID = %s", got)
	}
	if !strings.HasSuffix(src.Origin, filepath.Join("iPhoneOS26.4.sdk", "System", "Library", "Frameworks", "UIKit.framework", "Headers")) {
		t.Errorf("Origin = %s, want the Headers directory as xcrun names it", src.Origin)
	}
	// Root is the real directory, so rg can walk it.
	if realRoot, _ := filepath.EvalSymlinks(src.Root); realRoot != src.Root {
		t.Errorf("Root = %s is not a real path (%s)", src.Root, realRoot)
	}
	if _, err := os.Stat(filepath.Join(src.Root, "UIViewController.h")); err != nil {
		t.Errorf("the header is not below Root: %v", err)
	}

	// Request.SDK picks the SDK; a version in the coordinate wins over it.
	sim, err := a.Resolve(ctx, Request{Coordinate: coord(t, "sdk.apple:UIKit"), SDK: "iphonesimulator"})
	if err != nil || sim.Coordinate.Version != "iphonesimulator26.4" {
		t.Errorf("Resolve(--sdk iphonesimulator) = %+v, %v", sim, err)
	}
	pinned, err := a.Resolve(ctx, Request{Coordinate: coord(t, "sdk.apple:UIKit:iphoneos26.4"), SDK: "iphonesimulator"})
	if err != nil || pinned.Coordinate.Version != "iphoneos26.4" || pinned.Root != src.Root {
		t.Errorf("Resolve(coordinate with an SDK) = %+v, %v; want the iphoneos SDK", pinned, err)
	}

	// A macOS framework whose Headers is a symbolic link.
	mac, err := a.Resolve(ctx, Request{Coordinate: coord(t, "sdk.apple:AppKit"), SDK: "macosx"})
	if err != nil {
		t.Fatalf("Resolve(AppKit) error = %v", err)
	}
	if !strings.HasSuffix(mac.Root, filepath.Join("Versions", "A", "Headers")) {
		t.Errorf("AppKit Root = %s, want the real Versions/A/Headers directory", mac.Root)
	}
	if !strings.HasSuffix(mac.Origin, filepath.Join("AppKit.framework", "Headers")) {
		t.Errorf("AppKit Origin = %s, want the Headers link", mac.Origin)
	}
}

func TestAppleLocate(t *testing.T) {
	a, _ := newApple(t)
	loc, err := a.Locate(context.Background(), Request{Coordinate: coord(t, "sdk.apple:UIKit"), SDK: "iphoneos"})
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}
	if loc.Coordinate.String() != "sdk.apple:UIKit:iphoneos26.4" || !strings.HasSuffix(loc.Path, filepath.Join("UIKit.framework", "Headers")) {
		t.Errorf("Locate() = %+v", loc)
	}
}

func TestAppleErrors(t *testing.T) {
	a, _ := newApple(t)
	ctx := context.Background()
	tests := []struct {
		name string
		req  Request
		code string
		part string
	}{
		{"foreign group", Request{Coordinate: coord(t, "kn.platform:UIKit")}, errs.BadCoord, "cannot resolve kn.platform:UIKit"},
		{"unknown framework", Request{Coordinate: coord(t, "sdk.apple:Nope")}, errs.NoSources, `no framework "Nope" in the SDK iphoneos26.4`},
		{"swift-only framework", Request{Coordinate: coord(t, "sdk.apple:SwiftUI")}, errs.NoSources, "no Objective-C headers"},
		{"framework of another SDK", Request{Coordinate: coord(t, "sdk.apple:AppKit")}, errs.NoSources, `no framework "AppKit"`},
		{"unknown SDK", Request{Coordinate: coord(t, "sdk.apple:UIKit:watchos99")}, errs.NoSources, `the Apple SDK "watchos99" is not installed`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := a.Resolve(ctx, tt.req)
			if errs.Code(err) != tt.code || !strings.Contains(err.Error(), tt.part) {
				t.Errorf("Resolve() error = %v, want %s with %q", err, tt.code, tt.part)
			}
		})
	}

	// No Xcode on the machine.
	none := &AppleSDK{Runner: &testutil.Runner{}}
	if _, err := none.Resolve(ctx, Request{Coordinate: coord(t, "sdk.apple:UIKit")}); errs.Code(err) != errs.NoXcode {
		t.Errorf("Resolve() without xcrun: error = %v, want %s", err, errs.NoXcode)
	}
	if _, err := none.Locate(ctx, Request{Coordinate: coord(t, "sdk.apple:UIKit")}); errs.Code(err) != errs.NoXcode {
		t.Errorf("Locate() without xcrun: error = %v, want %s", err, errs.NoXcode)
	}
}
