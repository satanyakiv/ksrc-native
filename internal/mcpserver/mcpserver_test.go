package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

const (
	unModule    = "kn.platform:UserNotifications"
	unFileID    = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt"
	utilsFileID = "kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt"
	vcFileID    = "sdk.apple:UIKit:iphoneos26.4!/UIViewController.h"
)

// fixture is an adapter over a fake distribution, a fake Xcode and an empty
// cache. Only rg and the fake klib script run as processes.
type fixture struct {
	a      *adapter.Adapter
	dist   testutil.Distribution
	stdout *bytes.Buffer // what the adapter itself prints, outside a tool call
	stderr *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	testutil.RequireRipgrep(t)

	home := t.TempDir()
	dump := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	dist := testutil.NewDistribution(t, filepath.Join(home, ".konan"), "2.4.21")
	dist.AddPlatformKlib(t, "ios_arm64", "UserNotifications", dump)
	dist.AddPlatformKlib(t, "ios_simulator_arm64", "UserNotifications", dump)
	dist.AddStdlibSources(t, nil)

	x := testutil.NewXcode(t)
	ios := x.AddSDK(t, "iPhoneOS", "26.4")
	x.AddHeader(t, ios, "UIKit", "UIViewController.h", "@interface UIViewController : UIResponder\n- (void)presentViewController:(UIViewController *)vc;\n@end\n")
	mac := x.AddSDK(t, "MacOSX", "26.4")
	x.AddVersionedHeader(t, mac, "AppKit", "NSWindow.h", "@interface NSWindow : NSResponder\n@end\n")

	cacheDir := filepath.Join(t.TempDir(), "cache")
	f := &fixture{dist: dist, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	f.a = &adapter.Adapter{
		Runner: &testutil.Overlay{Base: executil.OS{}, Fakes: x.Fakes()},
		Stdout: f.stdout,
		Stderr: f.stderr,
		Stdin:  strings.NewReader(""),
		Getenv: func(key string) string {
			if key == cache.EnvDir {
				return cacheDir
			}
			return ""
		},
		UserHome:   home,
		IsTerminal: func() bool { return false },
	}
	return f
}

// connect serves opts over an in-memory transport and returns a client
// session.
func (f *fixture) connect(t *testing.T, opts Options) *mcp.ClientSession {
	t.Helper()
	if opts.Defaults.Project == "" {
		// An empty directory, so the repository's own files never matter.
		opts.Defaults.Project = t.TempDir()
	}
	server, err := New(f.a, opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) protocol error = %v", tool, err)
	}
	return res
}

// texts returns the text blocks of a result.
func texts(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	var out []string
	for _, c := range res.Content {
		text, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content %T is not text", c)
		}
		out = append(out, text.Text)
	}
	return out
}

func TestParseTools(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{"", "search cat deps"},
		{"  ", "search cat deps"},
		{"all", "search cat deps where"},
		{"where", "where"},
		{"where,search", "search where"},
		{" cat , cat,deps ,", "cat deps"},
	}
	for _, tt := range tests {
		got, err := ParseTools(tt.spec)
		if err != nil || strings.Join(got, " ") != tt.want {
			t.Errorf("ParseTools(%q) = %q, %v; want %q", tt.spec, got, err, tt.want)
		}
	}
	for _, spec := range []string{"clean", "search,doctor", ",", "ALL"} {
		if _, err := ParseTools(spec); errs.Code(err) != errs.BadArgs || !strings.Contains(err.Error(), " Try: ") {
			t.Errorf("ParseTools(%q) error = %v, want %s with a hint", spec, err, errs.BadArgs)
		}
	}

	// The caller may change the result without touching the defaults.
	got, _ := ParseTools("")
	got[0] = "changed"
	if DefaultTools[0] != ToolSearch {
		t.Error("ParseTools returned the DefaultTools slice itself")
	}
}

func TestListTools(t *testing.T) {
	f := newFixture(t)

	names := func(opts Options) string {
		res, err := f.connect(t, opts).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		var out []string
		for _, tool := range res.Tools {
			out = append(out, tool.Name)
		}
		return strings.Join(out, " ")
	}
	// The SDK lists tools sorted by name.
	if got := names(Options{}); got != "cat deps search" {
		t.Errorf("default tools = %q, want cat deps search", got)
	}
	if got := names(Options{Tools: AllTools}); got != "cat deps search where" {
		t.Errorf("all tools = %q, want cat deps search where", got)
	}
	if got := names(Options{Tools: []string{ToolWhere}}); got != "where" {
		t.Errorf("--tools=where = %q", got)
	}
	if _, err := New(f.a, Options{Tools: []string{"clean"}}); errs.Code(err) != errs.BadArgs {
		t.Errorf("New(unknown tool) error = %v, want %s", err, errs.BadArgs)
	}
}

func TestToolSchemasMirrorTheFlags(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{Tools: AllTools, Version: "9.8.7"})

	if info := session.InitializeResult(); info.ServerInfo.Name != "ksrc-native" || info.ServerInfo.Version != "9.8.7" ||
		!strings.Contains(info.Instructions, "kn.platform:<Framework>") {
		t.Errorf("server info = %+v, instructions = %q", info.ServerInfo, info.Instructions)
	}

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	common := []string{"konan_home", "kotlin_version", "project", "target", "sdk"}
	want := map[string]struct {
		required   string
		properties []string
	}{
		ToolSearch: {"pattern", append([]string{"pattern", "modules", "all_platform", "context", "rg_args"}, common...)},
		ToolCat:    {"file_id", append([]string{"file_id", "lines", "line_numbers"}, common...)},
		ToolDeps:   {"", common},
		ToolWhere:  {"coordinate", append([]string{"coordinate"}, common...)},
	}
	for _, tool := range res.Tools {
		w := want[tool.Name]
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s: input schema is %T", tool.Name, tool.InputSchema)
		}
		props, _ := schema["properties"].(map[string]any)
		if len(props) != len(w.properties) {
			t.Errorf("%s: %d properties, want %d: %v", tool.Name, len(props), len(w.properties), props)
		}
		for _, name := range w.properties {
			prop, _ := props[name].(map[string]any)
			if prop == nil || prop["description"] == "" || prop["description"] == nil {
				t.Errorf("%s: property %q is missing or has no description", tool.Name, name)
			}
		}
		if got := fmt.Sprint(schema["required"]); (w.required == "" && schema["required"] != nil) || (w.required != "" && got != "["+w.required+"]") {
			t.Errorf("%s: required = %v, want [%s]", tool.Name, schema["required"], w.required)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties = %v, want false", tool.Name, schema["additionalProperties"])
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s: has an output schema; results are plain text", tool.Name)
		}
		if tool.Description == "" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: description %q, annotations %+v; want a description and a read-only hint", tool.Name, tool.Description, tool.Annotations)
		}
	}
}

func TestSearchReturnsWhatTheCommandPrints(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{})
	args := map[string]any{"pattern": "class platform/UserNotifications/UNNotification ", "modules": []string{unModule}}

	// The first call builds the dump: the hits come first, the note second.
	res := call(t, session, ToolSearch, args)
	got := texts(t, res)
	if res.IsError || len(got) != 2 || !strings.Contains(got[1], "dumping kn.platform:UserNotifications:2.4.21 for ios_arm64") {
		t.Fatalf("first call: IsError = %v, content = %q; want the hits and a progress note", res.IsError, got)
	}
	want := unFileID + " 2:13:public open class platform/UserNotifications/UNNotification : platform/darwin/NSObject, platform/Foundation/NSCopyingProtocol, platform/Foundation/NSSecureCodingProtocol {\n"
	if got[0] != want {
		t.Errorf("text = %q\nwant   %q", got[0], want)
	}
	if res.StructuredContent != nil {
		t.Errorf("structured content = %v, want none", res.StructuredContent)
	}

	// The same request through the adapter, as the command line runs it.
	if err := f.a.Search(context.Background(), adapter.SearchRequest{Pattern: "class platform/UserNotifications/UNNotification ", Modules: []string{unModule}}); err != nil {
		t.Fatal(err)
	}
	if f.stdout.String() != got[0] {
		t.Errorf("MCP text = %q\nCLI stdout = %q", got[0], f.stdout.String())
	}

	// A cached search is one text block, and nothing leaks to the adapter's
	// own streams.
	f.stdout.Reset()
	f.stderr.Reset()
	res = call(t, session, ToolSearch, map[string]any{
		"pattern": "COMPANION OBJECT PLATFORM/USERNOTIFICATIONS/UNNOTIFICATION\\.", "modules": []string{unModule},
		"context": 1, "rg_args": []string{"-i"},
	})
	got = texts(t, res)
	if res.IsError || len(got) != 1 {
		t.Fatalf("cached call: IsError = %v, content = %q", res.IsError, got)
	}
	lines := strings.Split(strings.TrimSpace(got[0]), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], ":14:public final companion object platform/UserNotifications/UNNotification.Companion") {
		t.Errorf("text =\n%s\nwant a context line, the match and a context line", got[0])
	}
	if f.stdout.Len() != 0 || f.stderr.Len() != 0 {
		t.Errorf("a tool call wrote to the adapter's streams: %q, %q", f.stdout.String(), f.stderr.String())
	}
	if calls := f.dist.KlibCalls(t); len(calls) != 1 {
		t.Errorf("klib ran %d times, want 1", len(calls))
	}
}

func TestSearchOtherModulesAndNoMatches(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{})

	res := call(t, session, ToolSearch, map[string]any{"pattern": "presentViewController", "modules": []string{"sdk.apple:UIKit"}})
	if got := texts(t, res); res.IsError || len(got) != 1 || !strings.HasPrefix(got[0], vcFileID+" 2:9:") {
		t.Errorf("header search: IsError = %v, content = %q", res.IsError, got)
	}
	res = call(t, session, ToolSearch, map[string]any{"pattern": "NSWindow", "modules": []string{"sdk.apple:AppKit"}, "sdk": "macosx"})
	if got := texts(t, res); res.IsError || !strings.HasPrefix(got[0], "sdk.apple:AppKit:macosx26.4!/NSWindow.h 1:12:") {
		t.Errorf("header search with sdk: IsError = %v, content = %q", res.IsError, got)
	}

	// Without modules the stdlib answers.
	res = call(t, session, ToolSearch, map[string]any{"pattern": "fun <R> memScoped"})
	if got := texts(t, res); res.IsError || !strings.HasPrefix(got[0], utilsFileID+" 5:15:") {
		t.Errorf("search without modules: IsError = %v, content = %q", res.IsError, got)
	}

	// No match is a success whose text is the note.
	res = call(t, session, ToolSearch, map[string]any{"pattern": "NoSuchSymbolAnywhere", "modules": []string{"sdk.apple:UIKit"}})
	if got := texts(t, res); res.IsError || len(got) != 1 || !strings.HasPrefix(got[0], "ksrc-native: no matches for ") {
		t.Errorf("no match: IsError = %v, content = %q", res.IsError, got)
	}
}

func TestCatDepsAndWhere(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{Tools: AllTools})

	res := call(t, session, ToolCat, map[string]any{"file_id": vcFileID, "lines": "1,2", "line_numbers": true})
	want := "1\t@interface UIViewController : UIResponder\n2\t- (void)presentViewController:(UIViewController *)vc;\n"
	if got := texts(t, res); res.IsError || len(got) != 1 || got[0] != want {
		t.Errorf("cat: IsError = %v, content = %q, want %q", res.IsError, got, want)
	}
	res = call(t, session, ToolCat, map[string]any{"file_id": utilsFileID, "lines": "1"})
	if got := texts(t, res); res.IsError || got[0] != "package kotlinx.cinterop\n" {
		t.Errorf("stdlib cat: IsError = %v, content = %q", res.IsError, got)
	}

	res = call(t, session, ToolDeps, nil)
	got := texts(t, res)
	if res.IsError || len(got) != 1 {
		t.Fatalf("deps: IsError = %v, content = %q", res.IsError, got)
	}
	lines := strings.Split(strings.TrimSpace(got[0]), "\n")
	if len(lines) != 4 || lines[0] != "kn.platform:UserNotifications:2.4.21 (ios_arm64, dump cached: no)" ||
		!strings.HasPrefix(lines[1], "kn.stdlib:native:2.4.21 (extracted: yes, ") ||
		!strings.HasPrefix(lines[2], "sdk.apple:<Framework>:iphoneos26.4 (") || !strings.HasPrefix(lines[3], "sdk.apple:<Framework>:macosx26.4 (") {
		t.Errorf("deps text =\n%s", got[0])
	}

	res = call(t, session, ToolWhere, map[string]any{"coordinate": "kn.platform:usernotifications"})
	wantPath := filepath.Join(f.dist.Home, "klib", "platform", "ios_arm64", "org.jetbrains.kotlin.native.platform.UserNotifications") + "\n"
	if got := texts(t, res); res.IsError || got[0] != wantPath {
		t.Errorf("where: IsError = %v, content = %q, want %q", res.IsError, got, wantPath)
	}
}

func TestServerDefaultsAndPerCallOverrides(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{Tools: AllTools, Defaults: adapter.Common{Target: "ios_simulator_arm64", SDK: "macosx"}})

	// The defaults the server was started with apply to a call without them.
	res := call(t, session, ToolWhere, map[string]any{"coordinate": unModule})
	if got := texts(t, res); res.IsError || !strings.Contains(got[0], filepath.Join("platform", "ios_simulator_arm64")) {
		t.Errorf("where with the server target: IsError = %v, content = %q", res.IsError, got)
	}
	res = call(t, session, ToolWhere, map[string]any{"coordinate": "sdk.apple:AppKit"})
	if got := texts(t, res); res.IsError || !strings.Contains(got[0], "MacOSX26.4.sdk") {
		t.Errorf("where with the server SDK: IsError = %v, content = %q", res.IsError, got)
	}

	// An input of the call wins.
	res = call(t, session, ToolWhere, map[string]any{"coordinate": unModule, "target": "ios_arm64"})
	if got := texts(t, res); res.IsError || !strings.Contains(got[0], filepath.Join("platform", "ios_arm64")) {
		t.Errorf("where with its own target: IsError = %v, content = %q", res.IsError, got)
	}
	res = call(t, session, ToolWhere, map[string]any{"coordinate": "sdk.apple:UIKit", "sdk": "iphoneos"})
	if got := texts(t, res); res.IsError || !strings.Contains(got[0], "iPhoneOS26.4.sdk") {
		t.Errorf("where with its own SDK: IsError = %v, content = %q", res.IsError, got)
	}
}

func TestFailuresAreToolErrors(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{Tools: AllTools})
	tests := []struct {
		name   string
		tool   string
		args   map[string]any
		prefix string
		part   string
	}{
		{"unknown framework", ToolSearch, map[string]any{"pattern": "x", "modules": []string{"kn.platform:Nope"}}, "E_NO_SOURCES: ", `no platform library "Nope"`},
		{"unknown group", ToolSearch, map[string]any{"pattern": "x", "modules": []string{"com.example:lib"}}, "E_BAD_COORD: ", "com.example"},
		{"version that is not installed", ToolSearch, map[string]any{"pattern": "x", "modules": []string{unModule}, "kotlin_version": "1.9.0"}, "E_NO_KONAN: ", "installed: 2.4.21"},
		{"bad regular expression", ToolSearch, map[string]any{"pattern": "(", "modules": []string{"sdk.apple:UIKit"}}, "E_RG_FAILED: ", ""},
		{"file-id that is a path", ToolCat, map[string]any{"file_id": "UIViewController.h"}, "E_BAD_FILE_ID: ", "cannot parse the file-id"},
		{"missing file", ToolCat, map[string]any{"file_id": "sdk.apple:UIKit:iphoneos26.4!/Nope.h"}, "E_NO_FILE: ", "Nope.h"},
		{"bad line range", ToolCat, map[string]any{"file_id": vcFileID, "lines": "x"}, "E_BAD_LINES: ", "cannot parse the line range"},
		{"unknown SDK", ToolWhere, map[string]any{"coordinate": "sdk.apple:UIKit", "sdk": "watchos"}, "E_NO_SOURCES: ", `"watchos" is not installed`},
		{"distribution that does not exist", ToolDeps, map[string]any{"konan_home": filepath.Join(t.TempDir(), "nope")}, "E_NO_KONAN: ", "--konan-home"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, session, tt.tool, tt.args)
			got := texts(t, res)
			if !res.IsError || len(got) != 1 {
				t.Fatalf("IsError = %v, content = %q; want one error text", res.IsError, got)
			}
			if !strings.HasPrefix(got[0], tt.prefix) || !strings.Contains(got[0], tt.part) || !strings.Contains(got[0], " Try: ") {
				t.Errorf("text = %q, want prefix %q, %q and a hint", got[0], tt.prefix, tt.part)
			}
		})
	}

	// Input that does not fit the schema never reaches the adapter.
	for name, args := range map[string]map[string]any{
		"missing required input": {"modules": []string{unModule}},
		"unknown input":          {"pattern": "x", "module": unModule},
		"wrong type":             {"pattern": "x", "context": "two"},
	} {
		res := call(t, session, ToolSearch, args)
		if got := texts(t, res); !res.IsError || len(got) != 1 || !strings.Contains(got[0], "validating") {
			t.Errorf("%s: IsError = %v, content = %q; want a validation error", name, res.IsError, got)
		}
	}

	// A tool that is not served is a protocol error.
	limited := f.connect(t, Options{})
	if _, err := limited.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolWhere, Arguments: map[string]any{"coordinate": unModule}}); err == nil {
		t.Error("calling a tool that is not served succeeded")
	}
}

func TestLongOutputIsTruncatedAtALine(t *testing.T) {
	h := &handlers{a: &adapter.Adapter{}}
	line := strings.Repeat("x", 99) + "\n"
	res := h.call(func(a *adapter.Adapter) error {
		for i := 0; i < 2*MaxResultBytes/len(line); i++ {
			if _, err := a.Stdout.Write([]byte(line)); err != nil {
				return err
			}
		}
		return nil
	})
	text := res.Content[0].(*mcp.TextContent).Text
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	last := lines[len(lines)-1]
	if res.IsError || !strings.HasPrefix(last, "ksrc-native: output truncated at ") || !strings.Contains(last, fmt.Sprintf("of %d bytes", 2*MaxResultBytes/len(line)*len(line))) {
		t.Errorf("last line = %q, want a truncation note with the full size", last)
	}
	for i, l := range lines[:len(lines)-1] {
		if len(l) != 99 {
			t.Fatalf("line %d has %d bytes: a line was cut in the middle", i+1, len(l))
		}
	}
	if len(text) > MaxResultBytes+200 {
		t.Errorf("text has %d bytes, want about %d", len(text), MaxResultBytes)
	}

	// Short output is untouched; no output at all still yields a text.
	res = h.call(func(a *adapter.Adapter) error { _, err := a.Stdout.Write([]byte("one\n")); return err })
	if got := res.Content[0].(*mcp.TextContent).Text; got != "one\n" {
		t.Errorf("short output = %q", got)
	}
	res = h.call(func(*adapter.Adapter) error { return nil })
	if got := res.Content[0].(*mcp.TextContent).Text; got == "" || res.IsError {
		t.Errorf("no output: text = %q, IsError = %v", got, res.IsError)
	}

	// A failure keeps the notes and drops partial output.
	res = h.call(func(a *adapter.Adapter) error {
		a.Stdout.Write([]byte("partial\n"))
		a.Stderr.Write([]byte("ksrc-native: skipped: something\n"))
		return errs.New(errs.NoSources, "nothing to list", "run doctor")
	})
	if got := res.Content[0].(*mcp.TextContent).Text; !res.IsError || got != "ksrc-native: skipped: something\nE_NO_SOURCES: nothing to list. Try: run doctor" {
		t.Errorf("failure text = %q, IsError = %v", got, res.IsError)
	}
}

func TestRunEndsWhenTheClientCloses(t *testing.T) {
	f := newFixture(t)
	serverSide, clientSide := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), f.a, Options{}, serverSide) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("Run() after the client closed = %v, want nil", err)
	}

	// A cancelled context is a normal end too.
	ctx, cancel := context.WithCancel(context.Background())
	serverSide, _ = mcp.NewInMemoryTransports()
	go func() { done <- Run(ctx, f.a, Options{}, serverSide) }()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() after cancel = %v, want nil", err)
	}

	if err := Run(context.Background(), f.a, Options{Tools: []string{"nope"}}, serverSide); errs.Code(err) != errs.BadArgs {
		t.Errorf("Run(unknown tool) error = %v, want %s", err, errs.BadArgs)
	}
}

// A model fills rg_args and pattern, and text in a hostile repository can
// steer it. Neither may run a program or read a file outside the modules.
func TestSearchInputsAreUntrusted(t *testing.T) {
	f := newFixture(t)
	session := f.connect(t, Options{})
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "pre.sh")
	outside := filepath.Join(dir, "outside.txt")
	testutil.WriteFile(t, script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0o755)
	testutil.WriteFile(t, outside, []byte("hello-from-outside\n"), 0o644)

	for name, rgArgs := range map[string][]string{
		"a preprocessor":     {"--pre", script},
		"a path":             {outside},
		"files with matches": {"-l"},
		"list files":         {"--files"},
		"glob without value": {"-g"},
	} {
		res := call(t, session, ToolSearch, map[string]any{"pattern": "hello", "modules": []string{"sdk.apple:UIKit"}, "rg_args": rgArgs})
		got := texts(t, res)
		if !res.IsError || len(got) != 1 || !strings.HasPrefix(got[0], "E_BAD_ARGS: ") || !strings.Contains(got[0], "Try: these rg arguments are allowed: -i ") {
			t.Errorf("%s: IsError = %v, content = %q; want one E_BAD_ARGS text with the allowed arguments", name, res.IsError, got)
		}
	}

	// A pattern that looks like a flag is a pattern: a search without matches.
	for _, pattern := range []string{"--files", "--pre=" + script, "-l", "--no-json"} {
		res := call(t, session, ToolSearch, map[string]any{"pattern": pattern, "modules": []string{"sdk.apple:UIKit"}})
		got := texts(t, res)
		if res.IsError || len(got) != 1 || !strings.HasPrefix(got[0], "ksrc-native: no matches for ") {
			t.Errorf("pattern %q: IsError = %v, content = %q; want the no-matches note", pattern, res.IsError, got)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a tool call ran the preprocessor script")
	}

	// The allowed arguments still work.
	res := call(t, session, ToolSearch, map[string]any{"pattern": "PRESENTVIEWCONTROLLER", "modules": []string{"sdk.apple:UIKit"}, "rg_args": []string{"-i", "-g", "*Controller*", "--max-count", "1"}})
	if got := texts(t, res); res.IsError || len(got) != 1 || !strings.HasPrefix(got[0], vcFileID+" 2:9:") {
		t.Errorf("allowed rg_args: IsError = %v, content = %q", res.IsError, got)
	}
}
