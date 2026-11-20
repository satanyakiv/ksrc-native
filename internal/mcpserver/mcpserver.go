// Package mcpserver exposes the operations of internal/adapter as tools of a
// Model Context Protocol server.
//
// A tool takes the same inputs as the command of the same name, runs the
// same adapter method and returns what the command prints to stdout as plain
// text. A failure is a tool result with IsError set and the usual
// "E_CODE: message. Try: hint" line, so the model can read it and correct
// the call.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// Tool names. They equal the command names.
const (
	ToolSearch = "search"
	ToolCat    = "cat"
	ToolDeps   = "deps"
	ToolWhere  = "where"
)

// AllTools lists every tool in the order the server registers them.
var AllTools = []string{ToolSearch, ToolCat, ToolDeps, ToolWhere}

// DefaultTools are the tools served when none are named.
var DefaultTools = []string{ToolSearch, ToolCat, ToolDeps}

// ServerName is the implementation name the server reports.
const ServerName = "ksrc-native"

// E_MCP is the code of a failure of the server itself, not of a tool.
const codeMCP = "E_MCP"

// MaxResultBytes caps the text of one tool result. A search with thousands
// of hits would otherwise fill the context of the model that called it.
const MaxResultBytes = 256 * 1024

const instructions = `ksrc-native searches and reads what a Gradle dependency search does not reach in Kotlin Multiplatform iOS code.

Modules:
  kn.platform:<Framework>[:<kotlinVersion>]  Kotlin signatures of platform.<Framework> (UIKit, Foundation, ...)
  kn.stdlib:native[:<kotlinVersion>]         sources of the native stdlib and kotlinx.cinterop
  sdk.apple:<Framework>[:<sdk>]              Objective-C headers of an Xcode SDK

Flow: call search with a module, then call cat with the file-id of a hit.
Names in a platform dump are fully qualified with slashes: platform/UIKit/UIView, kotlin/String.`

// ParseTools turns the value of --tools into tool names. An empty value
// means the default tools and "all" means every tool. The result follows the
// order of AllTools and has no duplicates.
func ParseTools(spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "":
		return append([]string(nil), DefaultTools...), nil
	case "all":
		return append([]string(nil), AllTools...), nil
	}
	wanted := make(map[string]bool)
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !isTool(name) {
			return nil, errs.New(errs.BadArgs,
				fmt.Sprintf("unknown MCP tool %q", name),
				"use a comma-separated list of "+strings.Join(AllTools, ", ")+", or `all`")
		}
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return nil, errs.New(errs.BadArgs, "--tools names no tool",
			"use a comma-separated list of "+strings.Join(AllTools, ", ")+", or `all`")
	}
	var tools []string
	for _, name := range AllTools {
		if wanted[name] {
			tools = append(tools, name)
		}
	}
	return tools, nil
}

func isTool(name string) bool {
	for _, tool := range AllTools {
		if tool == name {
			return true
		}
	}
	return false
}

// Options configure a server.
type Options struct {
	// Version is reported to the client as the server version.
	Version string
	// Tools are the tools to serve; nil means DefaultTools.
	Tools []string
	// Defaults fill the common inputs a tool call leaves out. They come from
	// the flags `ksrc-native mcp` was started with.
	Defaults adapter.Common
}

// New returns a server whose tools run on a.
func New(a *adapter.Adapter, opts Options) (*mcp.Server, error) {
	tools := opts.Tools
	if tools == nil {
		tools = DefaultTools
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Title: "ksrc-native", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	h := &handlers{a: a, defaults: opts.Defaults}
	for _, name := range tools {
		switch name {
		case ToolSearch:
			mcp.AddTool(server, searchTool, h.search)
		case ToolCat:
			mcp.AddTool(server, catTool, h.cat)
		case ToolDeps:
			mcp.AddTool(server, depsTool, h.deps)
		case ToolWhere:
			mcp.AddTool(server, whereTool, h.where)
		default:
			return nil, errs.New(errs.BadArgs, fmt.Sprintf("unknown MCP tool %q", name),
				"use "+strings.Join(AllTools, ", "))
		}
	}
	return server, nil
}

// Run serves one session over the transport until the client closes it or
// ctx is cancelled. Both are a normal end.
func Run(ctx context.Context, a *adapter.Adapter, opts Options, transport mcp.Transport) error {
	server, err := New(a, opts)
	if err != nil {
		return err
	}
	err = server.Run(ctx, transport)
	if err == nil || ctx.Err() != nil || errors.Is(err, io.EOF) {
		return nil
	}
	return errs.Wrap(err, codeMCP, "the MCP session ended with an error: "+err.Error(),
		"check that the client speaks MCP over stdio: one JSON message per line")
}

// StdioTransport returns the transport of `ksrc-native mcp`: JSON messages,
// one per line, read from in and written to out.
func StdioTransport(in io.Reader, out io.Writer) mcp.Transport {
	return &mcp.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{out}}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
