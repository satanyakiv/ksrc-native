// Package xcode locates Apple SDKs and the Objective-C headers of their
// frameworks through the Xcode command-line tools.
package xcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// DefaultSDK is the SDK used when --sdk is not given.
const DefaultSDK = "iphoneos"

// SDK is one Apple SDK on disk.
type SDK struct {
	Name    string // canonical name with the version, for example iphoneos26.4
	Version string // for example 26.4; may be empty
	Path    string // the .sdk directory
}

// sdkHint names the SDKs people usually mean.
const sdkHint = "use iphoneos, iphonesimulator, macosx, or a full name from `ksrc-native deps` such as iphoneos26.4"

// ValidSDKName reports whether name has the form of an SDK name: a letter
// followed by letters, digits and dots, as in iphoneos or macosx26.4.
//
// xcrun also accepts a directory path after --sdk and answers with that
// path, so a name that is not checked would let a caller point the tool at
// any directory. A leading dash would be read by xcrun as a flag.
func ValidSDKName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || !((c >= '0' && c <= '9') || c == '.')) {
			return false
		}
	}
	return true
}

// BadSDKName is the error for a value that is not an SDK name. what names
// where the value came from, for example "--sdk".
func BadSDKName(code, what, name string) error {
	return errs.New(code, fmt.Sprintf("%s %q is not an Apple SDK name", what, name), sdkHint)
}

const installHint = "install Xcode, then select it with `sudo xcode-select -s /Applications/Xcode.app`; `ksrc-native doctor` shows what is found"

// Find resolves an SDK name such as iphoneos, iphonesimulator, macosx or
// iphoneos26.4 with `xcrun --sdk <name> --show-sdk-path`.
func Find(ctx context.Context, runner executil.Runner, name string) (SDK, error) {
	if name == "" {
		name = DefaultSDK
	}
	// The adapter checks the name first and knows where it came from; this
	// check is for every other caller.
	if !ValidSDKName(name) {
		return SDK{}, BadSDKName(errs.BadArgs, "the SDK", name)
	}
	xcrun, err := runner.LookPath("xcrun")
	if err != nil {
		return SDK{}, errs.New(errs.NoXcode, "xcrun is not on PATH, so no Apple SDK can be located", installHint)
	}

	var out bytes.Buffer
	err = runner.Run(ctx, executil.Command{Path: xcrun, Args: []string{"--sdk", name, "--show-sdk-path"}, Stdout: &out})
	if err != nil {
		return SDK{}, findError(name, err)
	}
	path := strings.TrimSpace(out.String())
	if info, statErr := os.Stat(path); path == "" || statErr != nil || !info.IsDir() {
		return SDK{}, errs.New(errs.NoXcode, fmt.Sprintf("xcrun reported the SDK path %q for %s, which is not a directory", path, name), installHint)
	}
	return describe(path, name), nil
}

// findError tells an unknown SDK name apart from a missing or broken Xcode.
func findError(name string, err error) error {
	if errs.Stopped(err) {
		return err
	}
	var exit *executil.ExitError
	if errors.As(err, &exit) {
		stderr := exit.Stderr
		if strings.Contains(stderr, "cannot be located") || strings.Contains(stderr, "unable to lookup item") {
			return errs.New(errs.NoSources,
				fmt.Sprintf("the Apple SDK %q is not installed", name),
				"run `ksrc-native deps` to see the installed SDKs; iOS SDKs need the full Xcode, not only the Command Line Tools")
		}
	}
	return errs.Wrap(err, errs.NoXcode, "xcrun cannot locate an Apple SDK: "+oneLine(err.Error()), installHint)
}

// describe reads the canonical name and version of the SDK at path from its
// SDKSettings.json. requested is the fallback name.
func describe(path, requested string) SDK {
	sdk := SDK{Name: requested, Path: path}
	data, err := os.ReadFile(filepath.Join(path, "SDKSettings.json"))
	if err != nil {
		return sdk
	}
	var settings struct {
		CanonicalName string `json:"CanonicalName"`
		Version       string `json:"Version"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return sdk
	}
	if settings.CanonicalName != "" {
		sdk.Name = settings.CanonicalName
	}
	sdk.Version = settings.Version
	return sdk
}

// List returns the SDKs of the selected developer directory, sorted by name.
// An SDK that is reachable under several directory names is listed once.
func List(ctx context.Context, runner executil.Runner) ([]SDK, error) {
	tool, err := runner.LookPath("xcode-select")
	if err != nil {
		return nil, errs.New(errs.NoXcode, "xcode-select is not on PATH, so no Apple SDK can be listed", installHint)
	}
	var out bytes.Buffer
	if err := runner.Run(ctx, executil.Command{Path: tool, Args: []string{"-p"}, Stdout: &out}); err != nil {
		if errs.Stopped(err) {
			return nil, err
		}
		return nil, errs.Wrap(err, errs.NoXcode, "xcode-select reports no developer directory: "+oneLine(err.Error()), installHint)
	}
	developer := strings.TrimSpace(out.String())

	// Xcode keeps SDKs per platform; the Command Line Tools keep them in one
	// directory.
	var dirs []string
	for _, pattern := range []string{
		filepath.Join(developer, "Platforms", "*.platform", "Developer", "SDKs", "*.sdk"),
		filepath.Join(developer, "SDKs", "*.sdk"),
	} {
		matches, _ := filepath.Glob(pattern)
		dirs = append(dirs, matches...)
	}

	seen := make(map[string]bool)
	var sdks []SDK
	for _, dir := range dirs {
		realDir, err := filepath.EvalSymlinks(dir)
		if err != nil || seen[realDir] {
			continue
		}
		seen[realDir] = true
		fallback := strings.ToLower(strings.TrimSuffix(filepath.Base(realDir), ".sdk"))
		sdks = append(sdks, describe(realDir, fallback))
	}
	sort.Slice(sdks, func(i, j int) bool { return sdks[i].Name < sdks[j].Name })
	return sdks, nil
}

const frameworkSuffix = ".framework"

func (s SDK) frameworksDir() string {
	return filepath.Join(s.Path, "System", "Library", "Frameworks")
}

// Frameworks lists the frameworks of the SDK that ship Objective-C headers,
// sorted by name. Swift-only frameworks have no Headers directory and are
// left out.
func (s SDK) Frameworks() []string {
	entries, err := os.ReadDir(s.frameworksDir())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), frameworkSuffix)
		if !ok || name == "" {
			continue
		}
		if isDir(filepath.Join(s.frameworksDir(), e.Name(), "Headers")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Headers describes the header directory of one framework.
type Headers struct {
	Framework string // canonical name, for example UIKit
	Dir       string // <sdk>/System/Library/Frameworks/<Framework>.framework/Headers
}

// FindHeaders locates the Headers directory of a framework. The name is
// matched without regard to case.
func (s SDK) FindHeaders(framework string) (Headers, error) {
	entries, err := os.ReadDir(s.frameworksDir())
	if err != nil {
		return Headers{}, errs.New(errs.NoSources,
			fmt.Sprintf("the SDK %s has no frameworks directory at %s", s.Name, s.frameworksDir()),
			"pass another --sdk; `ksrc-native deps` lists the installed SDKs")
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), frameworkSuffix)
		if !ok || !strings.EqualFold(name, framework) {
			continue
		}
		dir := filepath.Join(s.frameworksDir(), e.Name(), "Headers")
		if !isDir(dir) {
			return Headers{}, errs.New(errs.NoSources,
				fmt.Sprintf("the framework %s in %s has no Objective-C headers (it is a Swift-only framework)", name, s.Name),
				"Kotlin/Native imports Objective-C APIs only, so there is no platform."+name+" either; run `ksrc-native deps` to see what can be searched")
		}
		return Headers{Framework: name, Dir: dir}, nil
	}
	return Headers{}, errs.New(errs.NoSources,
		fmt.Sprintf("no framework %q in the SDK %s", framework, s.Name),
		suggest(framework, s.Frameworks()))
}

func suggest(want string, frameworks []string) string {
	const limit = 8
	lower := strings.ToLower(want)
	var near []string
	for _, name := range frameworks {
		n := strings.ToLower(name)
		if strings.Contains(n, lower) || strings.Contains(lower, n) {
			near = append(near, name)
		}
	}
	if len(near) == 0 {
		return fmt.Sprintf("use a framework name as in `#import <Framework/Header.h>` (%d have headers, for example Foundation, UIKit); another --sdk may have it", len(frameworks))
	}
	if len(near) > limit {
		near = near[:limit]
	}
	return "did you mean " + strings.Join(near, ", ") + "?"
}

// isDir follows symbolic links: on macOS SDKs, Headers is a link into
// Versions/Current.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
