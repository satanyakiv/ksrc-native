package provider

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/xcode"
)

// AppleGroup is the synthetic group of Apple SDK headers:
// sdk.apple:<Framework>[:<sdk>]. The version part is an SDK name, for
// example iphoneos or iphoneos26.4.
const AppleGroup = "sdk.apple"

// AppleSDK serves the Objective-C headers of a framework from an Xcode SDK.
// The headers are searched in place; nothing is copied or cached.
type AppleSDK struct {
	Runner executil.Runner
}

// Resolve implements Provider. The SDK is the version of the coordinate when
// present, else Request.SDK, else the default SDK.
func (a *AppleSDK) Resolve(ctx context.Context, req Request) (Source, error) {
	coord, headers, err := a.find(ctx, req)
	if err != nil {
		return Source{}, err
	}
	// On macOS SDKs, Headers is a symbolic link; rg needs the real directory.
	root := headers.Dir
	if realDir, err := filepath.EvalSymlinks(headers.Dir); err == nil {
		root = realDir
	}
	return Source{Coordinate: coord, Root: root, Origin: headers.Dir}, nil
}

// Locate implements Provider: the origin is the Headers directory.
func (a *AppleSDK) Locate(ctx context.Context, req Request) (Location, error) {
	coord, headers, err := a.find(ctx, req)
	if err != nil {
		return Location{}, err
	}
	return Location{Coordinate: coord, Path: headers.Dir}, nil
}

func (a *AppleSDK) find(ctx context.Context, req Request) (Coordinate, xcode.Headers, error) {
	coord := req.Coordinate
	if coord.Group != AppleGroup {
		return Coordinate{}, xcode.Headers{}, errs.New(errs.BadCoord,
			fmt.Sprintf("the Apple SDK provider cannot resolve %s", coord),
			"use sdk.apple:<Framework>[:<sdk>], for example sdk.apple:UIKit")
	}
	name := coord.Version
	if name == "" {
		name = req.SDK
	}
	sdk, err := xcode.Find(ctx, a.Runner, name)
	if err != nil {
		return Coordinate{}, xcode.Headers{}, err
	}
	headers, err := sdk.FindHeaders(coord.Name)
	if err != nil {
		return Coordinate{}, xcode.Headers{}, err
	}
	return Coordinate{Group: AppleGroup, Name: headers.Framework, Version: sdk.Name}, headers, nil
}
