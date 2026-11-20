package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
)

// CommonInput mirrors the global flags of the command line that a call may
// set. Every field is optional; an empty one takes the value the server was
// started with.
//
// There is no konan_home on purpose. The distribution holds the klib program
// the server runs, so a call that could name a directory could run a program
// of its choice. Only --konan-home of `ksrc-native mcp` and the KONAN_HOME
// variable choose it, and both belong to the person who starts the server.
type CommonInput struct {
	KotlinVersion string `json:"kotlin_version,omitempty" jsonschema:"Kotlin version of the distribution to use, for example 2.4.21; default: read from gradle/libs.versions.toml of the project, else the newest installed"`
	Project       string `json:"project,omitempty" jsonschema:"project directory whose gradle/libs.versions.toml names the Kotlin version; default: the directory the server runs in"`
	Target        string `json:"target,omitempty" jsonschema:"Kotlin/Native target of kn.platform modules, as an exact lower-case name such as ios_arm64 or ios_simulator_arm64; default: ios_arm64"`
	SDK           string `json:"sdk,omitempty" jsonschema:"Apple SDK of sdk.apple modules: iphoneos, iphonesimulator, macosx, or a full name such as iphoneos26.4, never a path; default: iphoneos"`
}

// common merges the input with the defaults of the server.
func (in CommonInput) common(defaults adapter.Common) adapter.Common {
	pick := func(value, fallback string) string {
		if value != "" {
			return value
		}
		return fallback
	}
	return adapter.Common{
		KonanHome:     defaults.KonanHome,
		KotlinVersion: pick(in.KotlinVersion, defaults.KotlinVersion),
		Project:       pick(in.Project, defaults.Project),
		Target:        pick(in.Target, defaults.Target),
		SDK:           pick(in.SDK, defaults.SDK),
	}
}

// SearchInput is the input of the search tool.
type SearchInput struct {
	Pattern     string   `json:"pattern" jsonschema:"ripgrep regular expression. Names in a platform dump are fully qualified with slashes, for example 'class platform/UIKit/UIApplication ' or 'fun openURL\\('"`
	Modules     []string `json:"modules,omitempty" jsonschema:"modules to search: kn.platform:<Framework>[:<kotlinVersion>], kn.stdlib:native[:<kotlinVersion>], sdk.apple:<Framework>[:<sdk>]. Without modules, the cached platform dumps and the stdlib are searched"`
	AllPlatform bool     `json:"all_platform,omitempty" jsonschema:"build and search every platform library of the target; takes minutes the first time"`
	Context     int      `json:"context,omitempty" jsonschema:"lines of context around each match"`
	RgArgs      []string `json:"rg_args,omitempty" jsonschema:"extra ripgrep arguments, one array item per word. Only these are allowed: -i -s -S -F -w -x -v -U -P --multiline-dotall, -g/--glob <glob>, --iglob <glob>, -m/--max-count <n>, -A/--after-context <n>, -B/--before-context <n>, --max-columns <n>. Anything else is an E_BAD_ARGS error"`
	CommonInput
}

// CatInput is the input of the cat tool.
type CatInput struct {
	FileID      string `json:"file_id" jsonschema:"file-id from a search hit, for example kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt"`
	Lines       string `json:"lines,omitempty" jsonschema:"line range A,B (1-based, inclusive), for example 1,60; default: the whole file"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"prefix every line with its number"`
	CommonInput
}

// DepsInput is the input of the deps tool.
type DepsInput struct {
	CommonInput
}

// WhereInput is the input of the where tool.
type WhereInput struct {
	Coordinate string `json:"coordinate" jsonschema:"module coordinate, for example kn.platform:UIKit, kn.stdlib:native or sdk.apple:UIKit"`
	CommonInput
}

// readOnly marks a tool that changes nothing the caller can observe (the
// cache it may fill is an implementation detail) and talks to no network.
func readOnly(title string) *mcp.ToolAnnotations {
	closedWorld := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld}
}

var (
	searchTool = &mcp.Tool{
		Name: ToolSearch,
		Description: "Search Kotlin/Native platform API signatures (platform.UIKit, platform.Foundation, ...), " +
			"the native stdlib sources and Apple SDK headers with a regular expression. " +
			"Prints one line per match: `<file-id> <line>:<col>:<text>`; context lines are `<file-id> <line>-<text>`. " +
			"Pass the file-id of a hit to `cat`. The first search of a kn.platform framework builds its dump and takes a few seconds.",
		Annotations: readOnly("Search Kotlin/Native and Apple SDK sources"),
	}
	catTool = &mcp.Tool{
		Name: ToolCat,
		Description: "Print a file named by a file-id from `search` output, whole or a line range. " +
			"Platform dumps have one file per class; top-level functions, constants and type aliases are in `_toplevel.kt`.",
		Annotations: readOnly("Read a file by file-id"),
	}
	depsTool = &mcp.Tool{
		Name: ToolDeps,
		Description: "List the modules that can be searched, one per line: the platform libraries of the target with the state of their cached dump, " +
			"the stdlib sources, and the installed Apple SDKs (an sdk.apple line stands for all frameworks of one SDK).",
		Annotations: readOnly("List searchable modules"),
	}
	whereTool = &mcp.Tool{
		Name: ToolWhere,
		Description: "Print the path a module is read from: the klib directory of a platform library, the stdlib sources zip " +
			"or the Headers directory of an SDK framework. Builds nothing.",
		Annotations: readOnly("Locate a module on disk"),
	}
)

// handlers binds the tools to one adapter.
type handlers struct {
	a        *adapter.Adapter
	defaults adapter.Common
	limit    int
}

func (h *handlers) search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, any, error) {
	return h.call(func(a *adapter.Adapter) error {
		return a.Search(ctx, adapter.SearchRequest{
			Common:      in.common(h.defaults),
			Pattern:     in.Pattern,
			Modules:     in.Modules,
			AllPlatform: in.AllPlatform,
			Context:     in.Context,
			RgArgs:      in.RgArgs,
		})
	}), nil, nil
}

func (h *handlers) cat(ctx context.Context, _ *mcp.CallToolRequest, in CatInput) (*mcp.CallToolResult, any, error) {
	return h.call(func(a *adapter.Adapter) error {
		return a.Cat(ctx, adapter.CatRequest{
			Common:  in.common(h.defaults),
			FileID:  in.FileID,
			Lines:   in.Lines,
			Numbers: in.LineNumbers,
		})
	}), nil, nil
}

func (h *handlers) deps(ctx context.Context, _ *mcp.CallToolRequest, in DepsInput) (*mcp.CallToolResult, any, error) {
	return h.call(func(a *adapter.Adapter) error {
		return a.Deps(ctx, adapter.DepsRequest{Common: in.common(h.defaults)})
	}), nil, nil
}

func (h *handlers) where(ctx context.Context, _ *mcp.CallToolRequest, in WhereInput) (*mcp.CallToolResult, any, error) {
	return h.call(func(a *adapter.Adapter) error {
		return a.Where(ctx, adapter.WhereRequest{Common: in.common(h.defaults), Coordinate: in.Coordinate})
	}), nil, nil
}

// errResultFull is what the result buffer answers once it is full. An
// operation that gets it stops writing; a search stops rg.
var errResultFull = errors.New("the tool result is full")

// truncationNote is the last line of a result that was cut.
func truncationNote(kept, limit int) string {
	return fmt.Sprintf("ksrc-native: output truncated at %d bytes, the limit of one result is %d; narrow the pattern, name fewer modules, or pass a line range\n", kept, limit)
}

// call runs one operation on a copy of the adapter that writes into buffers,
// and turns what it printed into a tool result:
//
//   - success: the text of stdout; notes from stderr (a build in progress, a
//     skipped source) follow as a second text block. When stdout is empty,
//     for example for a search without matches, the notes are the result.
//   - failure: IsError with the notes and the "E_CODE: message. Try: hint"
//     line. Partial output is dropped.
//
// All text blocks together stay within the limit of the server: the notes
// get an eighth of it, the truncation note is counted, and the text gets the
// rest. Output that does not fit ends the operation early and is a success
// with the note, not a failure.
//
// It never returns a Go error: a failed operation is a tool error the model
// can read, not a protocol error.
func (h *handlers) call(run func(a *adapter.Adapter) error) *mcp.CallToolResult {
	notesLimit := h.limit / 8
	stdout := &cappedBuffer{limit: h.limit - notesLimit - len(truncationNote(h.limit, h.limit)), stop: true}
	stderr := &cappedBuffer{limit: notesLimit}
	err := run(h.a.WithOutput(stdout, stderr))
	if stdout.truncated && errors.Is(err, errResultFull) {
		err = nil
	}

	notes := strings.TrimRight(stderr.String(), "\n")
	if err != nil {
		text := err.Error()
		if notes != "" && len(notes)+1+len(text) <= h.limit {
			text = notes + "\n" + text
		}
		if len(text) > h.limit {
			text = text[:h.limit]
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	}

	text := stdout.String()
	if stdout.truncated {
		text += truncationNote(len(text), h.limit)
	}
	switch {
	case text == "" && notes == "":
		text = "ksrc-native: no output"
	case text == "":
		text = notes
	case notes != "":
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}, &mcp.TextContent{Text: notes}}}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// cappedBuffer keeps the first limit bytes written to it, cut after the last
// complete line.
type cappedBuffer struct {
	limit int
	// stop makes Write fail with errResultFull once the buffer is full, so
	// the operation ends instead of producing output nobody reads. Without
	// it the rest is dropped silently, which suits notes.
	stop      bool
	buf       bytes.Buffer
	truncated bool
}

// Write implements io.Writer.
func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.truncated {
		return c.full(0, len(p))
	}
	room := c.limit - c.buf.Len()
	if len(p) <= room {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:room])
	c.truncated = true
	// Drop the line that was cut in the middle.
	if i := bytes.LastIndexByte(c.buf.Bytes(), '\n'); i >= 0 {
		c.buf.Truncate(i + 1)
	}
	return c.full(room, len(p))
}

// full is the answer to a write that did not fit: kept of the size bytes
// were taken.
func (c *cappedBuffer) full(kept, size int) (int, error) {
	if c.stop {
		return kept, errResultFull
	}
	return size, nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }
