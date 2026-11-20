package cli

import (
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
		"sdk.apple:<Framework>:iphoneos26.4 (1 frameworks with headers, ",
		"sdk.apple:<Framework>:macosx26.4 (1 frameworks with headers, ",
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
	if code, stdout, _ := h.run("clean", "-y"); code != 0 || !strings.Contains(stdout, "deleted 1 entries, ") || entries() != 0 {
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
	for _, part := range []string{"stdlib: ok - ", "(extracted: no)", "xcode: ok - SDK iphoneos26.4 at ", "(1 frameworks with headers)"} {
		if !strings.Contains(stdout, part) {
			t.Errorf("doctor output lacks %q:\n%s", part, stdout)
		}
	}

	// Xcode and the stdlib sources are optional: doctor still exits with 0.
	h2 := newHarness(t)
	h2.runner.Fakes = nil
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
