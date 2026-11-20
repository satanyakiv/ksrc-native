package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

const (
	utilsFileID = "kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt"
	vcFileID    = "sdk.apple:UIKit:iphoneos26.4!/UIViewController.h"
)

func TestSearchAndCatStdlibAndHeaders(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)

	code, stdout, stderr := h.run("search", "fun <R> memScoped", "--module", "kn.stdlib:native", "--kotlin-version", "2.4.21")
	if code != 0 || !strings.HasPrefix(stdout, utilsFileID+" 5:15:") {
		t.Fatalf("stdlib search: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = h.run("cat", utilsFileID, "--lines", "1")
	if code != 0 || stdout != "package kotlinx.cinterop\n" {
		t.Errorf("stdlib cat: code = %d, stdout = %q", code, stdout)
	}

	code, stdout, stderr = h.run("search", "presentViewController", "--module", "sdk.apple:UIKit")
	if code != 0 || !strings.HasPrefix(stdout, vcFileID+" 2:9:") || stderr != "" {
		t.Fatalf("header search: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = h.run("open", vcFileID, "--lines", "1")
	if code != 0 || stdout != "@interface UIViewController : UIResponder\n" {
		t.Errorf("header cat: code = %d, stdout = %q", code, stdout)
	}

	// --sdk is a global flag: it works before and after the command.
	for _, args := range [][]string{
		{"--sdk", "macosx", "search", "NSWindow", "--module", "sdk.apple:AppKit"},
		{"search", "NSWindow", "--module", "sdk.apple:AppKit", "--sdk", "macosx"},
		{"search", "NSWindow", "--module", "sdk.apple:AppKit:macosx"},
	} {
		code, stdout, stderr = h.run(args...)
		if code != 0 || !strings.HasPrefix(stdout, "sdk.apple:AppKit:macosx26.4!/NSWindow.h 1:12:") {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
	}
}

func TestSearchWithoutModuleAndAllPlatform(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)
	dump := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	h.dist.AddPlatformKlib(t, "ios_arm64", "OtherKit", []byte(strings.ReplaceAll(string(dump), "UserNotifications", "OtherKit")))

	// Without --module nothing is built: the stdlib alone answers.
	code, stdout, stderr := h.run("search", `memScoped\(|val UNErrorDomain`)
	if code != 0 || !strings.HasPrefix(stdout, utilsFileID+" ") || strings.Contains(stdout, "kn.platform") {
		t.Fatalf("search without --module: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 0 {
		t.Fatalf("search without --module ran klib %d times", len(calls))
	}

	// --all-platform builds every library of the target.
	code, stdout, stderr = h.run("search", "val UNErrorDomain", "--all-platform")
	if code != 0 || strings.Count(stdout, "!/ios_arm64/_toplevel.kt 14:14:") != 2 {
		t.Fatalf("--all-platform: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 2 {
		t.Errorf("--all-platform ran klib %d times, want 2", len(calls))
	}

	// Now the cached dumps are part of a search without --module.
	code, stdout, _ = h.run("search", `memScoped\(|val UNErrorDomain`)
	if code != 0 || strings.Count(stdout, "kn.platform:") != 2 || !strings.Contains(stdout, utilsFileID+" ") {
		t.Errorf("search without --module after a build: code = %d, stdout = %q", code, stdout)
	}
}

func TestWhereCommand(t *testing.T) {
	h := newHarness(t)
	zipPath := h.dist.AddStdlibSources(t, nil)

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"where", "kn.platform:UserNotifications"},
			filepath.Join(h.dist.Home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.UserNotifications")},
		{[]string{"where", "kn.stdlib:native"}, zipPath},
		{[]string{"where", "sdk.apple:UIKit"},
			filepath.Join(h.xcode.Developer, "Platforms", "iPhoneOS.platform", "Developer", "SDKs", "iPhoneOS26.4.sdk", "System", "Library", "Frameworks", "UIKit.framework", "Headers")},
		{[]string{"where", "sdk.apple:AppKit", "--sdk", "macosx"},
			filepath.Join(h.xcode.Developer, "Platforms", "MacOSX.platform", "Developer", "SDKs", "MacOSX26.4.sdk", "System", "Library", "Frameworks", "AppKit.framework", "Headers")},
	}
	for _, tt := range tests {
		code, stdout, stderr := h.run(tt.args...)
		if code != 0 || stdout != tt.want+"\n" || stderr != "" {
			t.Errorf("%q: code = %d, stderr = %q\nstdout = %q\nwant     %q", tt.args, code, stderr, stdout, tt.want+"\n")
		}
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("where ran klib %d times", len(calls))
	}
}

func TestDepsCommand(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)

	code, stdout, stderr := h.run("deps")
	if code != 0 || stderr != "" {
		t.Fatalf("deps: code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("deps printed %d lines, want 4:\n%s", len(lines), stdout)
	}
	for i, prefix := range []string{
		"kn.platform:UserNotifications:2.4.21 (ios_arm64, dump cached: no)",
		"kn.stdlib:native:2.4.21 (extracted: no, ",
		"sdk.apple:<Framework>:iphoneos26.4 (1 framework with headers, ",
		"sdk.apple:<Framework>:macosx26.4 (1 framework with headers, ",
	} {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("deps line %d = %q, want prefix %q", i+1, lines[i], prefix)
		}
	}

	// Without Xcode the rest is still listed and the exit code stays 0.
	h.runner.Fakes = nil
	h.runner.Hidden = []string{"xcrun", "xcode-select"}
	code, stdout, stderr = h.run("deps")
	if code != 0 || strings.Contains(stdout, "sdk.apple") || !strings.Contains(stderr, "ksrc-native: skipped: E_NO_XCODE: ") {
		t.Errorf("deps without Xcode: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestCleanCommand(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)

	code, stdout, _ := h.run("clean")
	if code != 0 || !strings.HasPrefix(stdout, "the cache is empty: ") {
		t.Errorf("clean of an empty cache: code = %d, stdout = %q", code, stdout)
	}

	if code, _, stderr := h.run("search", "x", "--module", unModule, "--module", "kn.stdlib:native"); code != 0 {
		t.Fatalf("search: code = %d, stderr = %q", code, stderr)
	}
	entries := func() int {
		platform, _ := filepath.Glob(filepath.Join(h.cacheDir, "platform-dumps-v1", "*", "meta.json"))
		stdlib, _ := filepath.Glob(filepath.Join(h.cacheDir, "stdlib-v1", "*", "meta.json"))
		return len(platform) + len(stdlib)
	}
	if entries() != 2 {
		t.Fatalf("the cache holds %d entries, want 2", entries())
	}

	// --dry-run, and no terminal without -y: a preview only.
	for _, args := range [][]string{{"clean", "--dry-run"}, {"clean"}, {"clean", "--dry-run", "-y"}} {
		code, stdout, stderr := h.run(args...)
		if code != 0 || !strings.Contains(stdout, "kn.platform:UserNotifications:2.4.21 (ios_arm64) ") ||
			!strings.Contains(stdout, "total: 2 entries, ") || !strings.Contains(stdout, "nothing deleted") {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		if entries() != 2 {
			t.Fatalf("%q deleted entries", args)
		}
	}

	// On a terminal the answer decides.
	h.a.IsTerminal = func() bool { return true }
	h.a.Stdin = strings.NewReader("n\n")
	if code, stdout, _ := h.run("clean"); code != 0 || !strings.HasSuffix(stdout, "? [y/N] nothing deleted\n") || entries() != 2 {
		t.Errorf("clean answered with n: code = %d, stdout = %q, %d entries", code, stdout, entries())
	}
	h.a.Stdin = strings.NewReader("yes\n")
	if code, stdout, _ := h.run("clean"); code != 0 || !strings.Contains(stdout, "deleted 2 entries, ") || entries() != 0 {
		t.Errorf("clean answered with yes: code = %d, stdout = %q, %d entries", code, stdout, entries())
	}

	// -y needs no terminal.
	h.a.IsTerminal = func() bool { return false }
	h.a.Stdin = strings.NewReader("")
	if code, _, stderr := h.run("search", "x", "--module", unModule); code != 0 {
		t.Fatalf("search: code = %d, stderr = %q", code, stderr)
	}
	if code, stdout, _ := h.run("clean", "-y"); code != 0 || !strings.Contains(stdout, "deleted 1 entry, ") || entries() != 0 {
		t.Errorf("clean -y: code = %d, stdout = %q, %d entries", code, stdout, entries())
	}
	if _, err := os.Stat(h.cacheDir); err != nil {
		t.Errorf("clean removed the cache root itself: %v", err)
	}
}

func TestDoctorReportsStdlibAndXcode(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)

	code, stdout, stderr := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor: code = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, part := range []string{"stdlib: ok - ", "(extracted: no)", "xcode: ok - SDK iphoneos26.4 at ", "(1 framework with headers)"} {
		if !strings.Contains(stdout, part) {
			t.Errorf("doctor output lacks %q:\n%s", part, stdout)
		}
	}

	// Xcode and the stdlib sources are optional: doctor still exits with 0.
	h2 := newHarness(t)
	h2.runner.Fakes = withJava(nil)
	h2.runner.Hidden = []string{"xcrun", "xcode-select"}
	code, stdout, _ = h2.run("doctor")
	if code != 0 || !strings.Contains(stdout, "stdlib: unavailable - ") || !strings.Contains(stdout, "xcode: unavailable - E_NO_XCODE: ") {
		t.Errorf("doctor without the optional parts: code = %d\n%s", code, stdout)
	}
}

func TestNewCommandErrors(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)
	tests := []struct {
		name   string
		args   []string
		prefix string
		part   string
	}{
		{"where without a coordinate", []string{"where"}, "E_BAD_ARGS: ", "where takes one coordinate, got 0"},
		{"where with an unknown group", []string{"where", "com.example:lib"}, "E_BAD_COORD: ", "com.example"},
		{"where with an unknown framework", []string{"where", "kn.platform:Nope"}, "E_NO_SOURCES: ", `"Nope"`},
		{"deps with an argument", []string{"deps", "extra"}, "E_BAD_ARGS: ", "unknown command"},
		{"clean with an argument", []string{"clean", "extra"}, "E_BAD_ARGS: ", "unknown command"},
		{"swift-only framework", []string{"search", "x", "--module", "sdk.apple:SwiftUI"}, "E_NO_SOURCES: ", "no Objective-C headers"},
		{"unknown SDK", []string{"search", "x", "--module", "sdk.apple:UIKit", "--sdk", "watchos"}, "E_NO_SOURCES: ", `"watchos" is not installed`},
		{"two Kotlin versions", []string{"search", "x", "--module", unModule + ":2.4.21", "--module", "kn.stdlib:native:2.4.10"}, "E_BAD_COORD: ", "two Kotlin versions"},
		{"stdlib module that does not exist", []string{"search", "x", "--module", "kn.stdlib:jvm"}, "E_NO_SOURCES: ", `no module "jvm"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tt.args...)
			last := lastLine(stderr)
			if code != 1 || stdout != "" || !strings.HasPrefix(last, tt.prefix) || !strings.Contains(last, tt.part) || !strings.Contains(last, " Try: ") {
				t.Errorf("code = %d, stdout = %q, last stderr line = %q; want 1, nothing, %q with %q and a hint", code, stdout, last, tt.prefix, tt.part)
			}
		})
	}

	// Xcode is absent.
	h.runner.Fakes = nil
	h.runner.Hidden = []string{"xcrun", "xcode-select"}
	code, _, stderr := h.run("search", "x", "--module", "sdk.apple:UIKit")
	if code != 1 || !strings.HasPrefix(lastLine(stderr), "E_NO_XCODE: ") {
		t.Errorf("search without Xcode: code = %d, stderr = %q", code, stderr)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func TestHelpListsAllCommandsAndFlags(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("--help")
	if code != 0 {
		t.Fatalf("--help: code = %d", code)
	}
	for _, part := range []string{"search", "cat", "deps", "where", "doctor", "clean", "--sdk", "kn.stdlib:native", "sdk.apple:<Framework>"} {
		if !strings.Contains(stdout, part) {
			t.Errorf("help lacks %q:\n%s", part, stdout)
		}
	}
	// A back-quoted word in a flag description becomes the name of the flag's
	// value in the help, so only a real placeholder may be quoted.
	for _, part := range []string{"--konan-home path ", "--kotlin-version string", "--project string", "--target string", "--sdk string"} {
		if !strings.Contains(stdout, part) {
			t.Errorf("help lacks %q:\n%s", part, stdout)
		}
	}
	if _, stdout, _ := h.run("mcp", "--help"); !strings.Contains(stdout, "--tools list ") {
		t.Errorf("mcp --help does not name the value of --tools:\n%s", stdout)
	}

	code, stdout, _ = h.run("clean", "--help")
	if code != 0 || !strings.Contains(stdout, "--dry-run") || !strings.Contains(stdout, "--yes") {
		t.Errorf("clean --help: code = %d\n%s", code, stdout)
	}
}

// The arguments after "--" are checked against the allow-list of
// search.NormalizeExtraArgs before anything runs.
func TestSearchRgArgumentsFromTheCommandLine(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "pre.sh")
	outside := filepath.Join(dir, "outside.txt")
	testutil.WriteFile(t, script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0o755)
	testutil.WriteFile(t, outside, []byte("hello-from-outside\n"), 0o644)

	refused := []struct {
		name string
		args []string
		part string
	}{
		{"a preprocessor", []string{"search", "presentViewController", "--module", "sdk.apple:UIKit", "--", "--pre", script}, `"--pre"`},
		{"a path", []string{"search", "hello", "--module", "sdk.apple:UIKit", "--", outside}, "a path cannot be added"},
		{"files with matches", []string{"search", "x", "--module", "sdk.apple:UIKit", "--", "-l"}, `"-l"`},
		{"glob without a value", []string{"search", "x", "--module", "sdk.apple:UIKit", "--", "-g"}, "-g needs a value"},
		{"a second --", []string{"search", "hello", "--module", "sdk.apple:UIKit", "--", "-i", "--", outside}, `"--"`},
		{"a platform module", []string{"search", "x", "--module", unModule, "--", "--files"}, `"--files"`},
	}
	for _, tt := range refused {
		t.Run("refused/"+tt.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tt.args...)
			last := lastLine(stderr)
			if code != 1 || stdout != "" || !strings.HasPrefix(last, "E_BAD_ARGS: ") || !strings.Contains(last, tt.part) || !strings.Contains(last, "Try: these rg arguments are allowed: -i ") {
				t.Errorf("code = %d, stdout = %q, last stderr line = %q; want 1, nothing, E_BAD_ARGS with %q and the allowed arguments", code, stdout, last, tt.part)
			}
		})
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a refused search ran the preprocessor script")
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a refused search built %d dumps, want 0", len(calls))
	}

	const header = "sdk.apple:UIKit:iphoneos26.4!/UIViewController.h "
	allowed := []struct {
		name   string
		args   []string
		prefix string
	}{
		{"ignore case", []string{"search", "PRESENTVIEWCONTROLLER", "--module", "sdk.apple:UIKit", "--", "-i"}, header + "2:9:"},
		{"fixed string", []string{"search", "(void)presentViewController:(", "--module", "sdk.apple:UIKit", "--", "-F"}, header + "2:3:"},
		{"glob", []string{"search", "@interface", "--module", "sdk.apple:UIKit", "--", "-g", "*Controller*"}, header + "1:1:"},
		{"max count", []string{"search", "UIViewController", "--module", "sdk.apple:UIKit", "--", "--max-count", "1"}, header + "1:12:"},
		{"context and a glob", []string{"search", "class platform/UserNotifications/UNNotification ", "--module", unModule, "--context", "1", "--", "-g", "UNNotification.kt"}, unFileID + " 1-"},
	}
	for _, tt := range allowed {
		t.Run("allowed/"+tt.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tt.args...)
			if code != 0 || !strings.HasPrefix(stdout, tt.prefix) {
				t.Errorf("code = %d, stdout = %q, stderr = %q; want 0 and a first line with prefix %q", code, stdout, stderr, tt.prefix)
			}
		})
	}
}

// --target and --sdk are checked before a provider runs: a target must be
// the exact name of a target of the distribution, an SDK must be a name.
func TestTargetAndSDKFromTheCommandLine(t *testing.T) {
	h := newHarness(t)
	sdkDir := t.TempDir()
	tests := []struct {
		name   string
		args   []string
		prefix string
		part   string
	}{
		{"target with a path", []string{"search", "x", "--module", unModule, "--target", "../platform/ios_arm64"}, "E_NO_SOURCES: ", "pass --target with one of: ios_arm64"},
		{"target in another case", []string{"where", unModule, "--target", "IOS_ARM64"}, "E_NO_SOURCES: ", `target "IOS_ARM64"`},
		{"target in another case, search", []string{"search", "x", "--module", unModule, "--target", "IOS_ARM64"}, "E_NO_SOURCES: ", `target "IOS_ARM64"`},
		{"target for all platform libraries", []string{"search", "x", "--all-platform", "--target", "./ios_arm64"}, "E_NO_SOURCES: ", "pass --target with one of: ios_arm64"},
		{"target of deps", []string{"deps", "--target", "ios_arm64/"}, "E_NO_SOURCES: ", "pass --target with one of: ios_arm64"},
		{"target segment of a file-id", []string{"cat", "kn.platform:UserNotifications:2.4.21!/IOS_ARM64/UNNotification.kt"}, "E_NO_SOURCES: ", `target "IOS_ARM64"`},
		{"SDK that is a directory", []string{"search", "x", "--module", "sdk.apple:UIKit", "--sdk", sdkDir}, "E_BAD_ARGS: ", "--sdk"},
		{"SDK that is /tmp", []string{"where", "sdk.apple:UIKit", "--sdk", "/tmp"}, "E_BAD_ARGS: ", `"/tmp" is not an Apple SDK name`},
		{"SDK that looks like a flag", []string{"where", "sdk.apple:UIKit", "--sdk=--show-sdk-version"}, "E_BAD_ARGS: ", "is not an Apple SDK name"},
		{"SDK in a coordinate", []string{"where", "sdk.apple:UIKit:-iphoneos"}, "E_BAD_COORD: ", "is not an Apple SDK name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tt.args...)
			last := lastLine(stderr)
			if code != 1 || stdout != "" || !strings.HasPrefix(last, tt.prefix) || !strings.Contains(last, tt.part) || !strings.Contains(last, " Try: ") {
				t.Errorf("code = %d, stdout = %q, last stderr line = %q; want 1, nothing, %q with %q and a hint", code, stdout, last, tt.prefix, tt.part)
			}
		})
	}
	if calls := h.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("the refused commands built %d dumps, want 0", len(calls))
	}

	// Nothing was written to the cache, so the next search builds a clean
	// entry and finds the class.
	code, stdout, stderr := h.run("search", "class platform/UserNotifications/UNNotification ", "--module", unModule)
	if code != 0 || !strings.HasPrefix(stdout, unFileID+" 2:13:") {
		t.Errorf("search after the refused targets: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

// --target has no fixed default: a flag that always said ios_arm64 would
// look like a target the caller named, and a Linux distribution has none.
func TestTargetIsPickedWhenTheFlagIsNotGiven(t *testing.T) {
	h := newHarness(t)
	home := t.TempDir()
	linux := testutil.NewDistributionFor(t, filepath.Join(home, ".konan"), "linux-x86_64", "2.4.21")
	linux.AddPlatformKlib(t, "linux_x64", "posix", testutil.ReadFixture(t, testutil.UserNotificationsDump))
	h.a.UserHome = home
	h.a.GOOS, h.a.GOARCH = "linux", "amd64"

	if code, stdout, stderr := h.run("deps"); code != 0 || stdout != "kn.platform:posix:2.4.21 (linux_x64, dump cached: no)\n" || stderr != "" {
		t.Errorf("deps on Linux: code = %d, stdout = %q, stderr = %q; want the posix line of linux_x64", code, stdout, stderr)
	}
	if code, stdout, stderr := h.run("doctor"); code != 0 || !strings.Contains(stdout, "platform: ok - 1 library for linux_x64, the host target") {
		t.Errorf("doctor on Linux: code = %d, stderr = %q, stdout:\n%s", code, stderr, stdout)
	}
	if code, stdout, _ := h.run("search", "val UNErrorDomain", "--module", "kn.platform:posix"); code != 0 || !strings.HasPrefix(stdout, "kn.platform:posix:2.4.21!/linux_x64/_toplevel.kt ") {
		t.Errorf("search on Linux: code = %d, stdout = %q; want a hit in linux_x64", code, stdout)
	}
	if code, stdout, stderr := h.run("deps", "--target", "ios_arm64"); code != 1 || stdout != "" || !strings.HasPrefix(stderr, "E_NO_SOURCES: ") {
		t.Errorf("deps --target ios_arm64 on Linux: code = %d, stdout = %q, stderr = %q; want E_NO_SOURCES", code, stdout, stderr)
	}

	_, help, _ := h.run("--help")
	if !strings.Contains(help, "--target") || !strings.Contains(help, "default: ios_arm64, or the host target") {
		t.Errorf("--help does not say how the target is picked:\n%s", help)
	}
}

// docs/cli-api.md lists these forms of --lines. The parser has its own
// tests; this one pins what the command prints for each.
func TestCatLineRangeForms(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("search", "x", "--module", unModule); code != 0 {
		t.Fatalf("search: code = %d, stderr = %q", code, stderr)
	}
	_, whole, _ := h.run("cat", unFileID)
	lines := strings.SplitAfter(whole, "\n")
	lines = lines[:len(lines)-1]
	if len(lines) < 6 {
		t.Fatalf("the fixture file has %d lines, the test needs 6", len(lines))
	}
	last := fmt.Sprint(len(lines))

	tests := []struct {
		value string
		want  string
	}{
		{"2,4", strings.Join(lines[1:4], "")},
		{"2-4", strings.Join(lines[1:4], "")},
		{"2:4", strings.Join(lines[1:4], "")},
		{"3", lines[2]},
		{"5,", strings.Join(lines[4:], "")},
		{"5-", strings.Join(lines[4:], "")},
		{"5:", strings.Join(lines[4:], "")},
		{"5,99999", strings.Join(lines[4:], "")},
		{last + "," + last, lines[len(lines)-1]},
	}
	for _, tt := range tests {
		if code, stdout, stderr := h.run("cat", unFileID, "--lines", tt.value); code != 0 || stdout != tt.want {
			t.Errorf("cat --lines %s: code = %d, stderr = %q, stdout = %q\nwant %q", tt.value, code, stderr, stdout, tt.want)
		}
	}
	for _, value := range []string{"0,5", "5,2", "1,2,3", ",5", "a", "99999", "99999,"} {
		if code, stdout, stderr := h.run("cat", unFileID, "--lines", value); code != 1 || stdout != "" || !strings.HasPrefix(stderr, "E_BAD_LINES: ") {
			t.Errorf("cat --lines %s: code = %d, stdout = %q, stderr = %q; want E_BAD_LINES", value, code, stdout, stderr)
		}
	}
}

// The two rules of a pattern that docs/cli-api.md gives.
func TestSearchPatternRules(t *testing.T) {
	h := newHarness(t)
	for name, pattern := range map[string]string{"empty": "", "only white space": " \t "} {
		if code, stdout, stderr := h.run("search", pattern, "--module", unModule); code != 1 || stdout != "" || !strings.HasPrefix(stderr, "E_BAD_ARGS: the search pattern is empty") {
			t.Errorf("search with a pattern that is %s: code = %d, stdout = %q, stderr = %q; want E_BAD_ARGS", name, code, stdout, stderr)
		}
	}
	if code, _, stderr := h.run("search", "-foo", "--module", unModule); code != 1 || !strings.HasPrefix(stderr, "E_BAD_ARGS: ") {
		t.Errorf("search -foo: code = %d, stderr = %q; want E_BAD_ARGS, a pattern cannot start with a dash", code, stderr)
	}
	if code, _, stderr := h.run("search", "--module", unModule, "--", "-foo"); code != 1 || !strings.HasPrefix(stderr, "E_BAD_ARGS: ") {
		t.Errorf("search -- -foo: code = %d, stderr = %q; want E_BAD_ARGS, the place after -- belongs to rg", code, stderr)
	}
	// The two spellings the doc offers reach ripgrep as a pattern.
	for _, pattern := range []string{"[-]>", `\->`} {
		code, _, stderr := h.run("search", pattern, "--module", unModule)
		if code != 0 || strings.Contains(stderr, "E_") {
			t.Errorf("search %q: code = %d, stderr = %q; want a search that ran", pattern, code, stderr)
		}
	}
}
