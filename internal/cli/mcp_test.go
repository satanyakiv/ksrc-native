package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// startMCP runs `ksrc-native <args...>` (an mcp command line) on a copy of
// the harness adapter whose stdin and stdout are pipes, and connects an MCP
// client to the other ends. It returns the session and a function that
// closes it and reports the exit code of the command.
func (h *harness) startMCP(args ...string) (*mcp.ClientSession, func() int) {
	h.t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()

	a := *h.a
	a.Stdin, a.Stdout = serverIn, serverOut
	args = append([]string{"--project", h.t.TempDir()}, args...)
	exit := make(chan int, 1)
	go func() {
		code := Run(context.Background(), args, &a)
		serverOut.Close()
		exit <- code
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	h.t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		h.t.Fatalf("cannot connect to `ksrc-native %s`: %v\nstderr: %s", strings.Join(args, " "), err, h.err.String())
	}
	stop := func() int {
		session.Close()
		select {
		case code := <-exit:
			return code
		case <-time.After(10 * time.Second):
			h.t.Fatal("the mcp command did not exit after the client closed the session")
			return -1
		}
	}
	return session, stop
}

func toolNames(t *testing.T, session *mcp.ClientSession) string {
	t.Helper()
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return strings.Join(names, " ")
}

func TestMCPCommandServesTheDefaultTools(t *testing.T) {
	h := newHarness(t)
	h.dist.AddStdlibSources(t, nil)
	session, stop := h.startMCP("mcp")

	if got := toolNames(t, session); got != "cat deps search" {
		t.Errorf("tools/list = %q, want cat deps search", got)
	}

	// A search over MCP returns the line the command prints.
	const pattern = "class platform/UserNotifications/UNNotification "
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": pattern, "modules": []string{unModule}},
	})
	if err != nil || res.IsError {
		t.Fatalf("tools/call search: %v, result %+v", err, res)
	}
	viaMCP := res.Content[0].(*mcp.TextContent).Text

	code, viaCLI, stderr := h.run("search", pattern, "--module", unModule)
	if code != 0 {
		t.Fatalf("search: code = %d, stderr = %q", code, stderr)
	}
	if viaMCP != viaCLI || viaCLI != unFileID+" 2:13:"+unHeader+"\n" {
		t.Errorf("MCP text  = %q\nCLI stdout = %q", viaMCP, viaCLI)
	}

	// The file-id of the hit feeds the cat tool.
	fileID, _, _ := strings.Cut(viaMCP, " ")
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "cat",
		Arguments: map[string]any{"file_id": fileID, "lines": "2"},
	})
	if err != nil || res.IsError || res.Content[0].(*mcp.TextContent).Text != unHeader+"\n" {
		t.Errorf("tools/call cat: %v, result %+v", err, res)
	}

	// A failing tool call is a result the model can read.
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"pattern": "x", "modules": []string{"kn.platform:Nope"}},
	})
	if err != nil || !res.IsError || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, "E_NO_SOURCES: ") {
		t.Errorf("tools/call with an unknown framework: %v, result %+v", err, res)
	}

	if code := stop(); code != 0 {
		t.Errorf("exit code after the client closed = %d, want 0; stderr: %s", code, h.err.String())
	}
}

func TestMCPCommandToolsFlagAndGlobalFlags(t *testing.T) {
	h := newHarness(t)

	session, stop := h.startMCP("mcp", "--tools=all")
	if got := toolNames(t, session); got != "cat deps search where" {
		t.Errorf("--tools=all: tools/list = %q", got)
	}
	if code := stop(); code != 0 {
		t.Errorf("exit code = %d", code)
	}

	// Global flags of the command line are the defaults of every call.
	session, stop = h.startMCP("--sdk", "macosx", "mcp", "--tools", "where")
	if got := toolNames(t, session); got != "where" {
		t.Errorf("--tools where: tools/list = %q", got)
	}
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "where",
		Arguments: map[string]any{"coordinate": "sdk.apple:AppKit"},
	})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "MacOSX26.4.sdk") {
		t.Errorf("where with --sdk macosx as the server default: %v, result %+v", err, res)
	}
	if code := stop(); code != 0 {
		t.Errorf("exit code = %d", code)
	}

	// --konan-home is a start flag only. A call cannot name a distribution,
	// because the server runs the klib program found in it.
	other := testutil.NewDistribution(t, filepath.Join(t.TempDir(), "elsewhere"), "2.4.10")
	other.AddPlatformKlib(t, "ios_arm64", "UserNotifications", testutil.ReadFixture(t, testutil.UserNotificationsDump))
	session, stop = h.startMCP("--konan-home", other.Home, "mcp", "--tools", "where")
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "where",
		Arguments: map[string]any{"coordinate": unModule},
	})
	if err != nil || res.IsError || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, other.Home+string(filepath.Separator)) {
		t.Errorf("where with --konan-home as the server default: %v, result %+v", err, res)
	}
	res, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "where",
		Arguments: map[string]any{"coordinate": unModule, "konan_home": h.dist.Home},
	})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "konan_home") {
		t.Errorf("where with a konan_home input: %v, result %+v; want a validation error", err, res)
	}
	if code := stop(); code != 0 {
		t.Errorf("exit code = %d", code)
	}
}

func TestMCPCommandErrors(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		args []string
		part string
	}{
		{[]string{"mcp", "--tools=clean"}, `unknown MCP tool "clean"`},
		{[]string{"mcp", "--tools=,"}, "--tools names no tool"},
		{[]string{"mcp", "extra"}, "unknown command"},
	}
	for _, tt := range tests {
		code, stdout, stderr := h.run(tt.args...)
		last := lastLine(stderr)
		if code != 1 || stdout != "" || !strings.HasPrefix(last, "E_BAD_ARGS: ") || !strings.Contains(last, tt.part) {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %q", tt.args, code, stdout, stderr)
		}
	}

	// Stdin at its end is a session that never started: a clean exit, and
	// nothing but protocol messages on stdout.
	code, stdout, stderr := h.run("mcp")
	if code != 0 || stdout != "" {
		t.Errorf("mcp with an empty stdin: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, _ = h.run("mcp", "--help")
	if code != 0 || !strings.Contains(stdout, "--tools") || !strings.Contains(stdout, "search, cat and deps by default") ||
		!strings.Contains(stdout, "--konan-home is not an input of a call") {
		t.Errorf("mcp --help: code = %d\n%s", code, stdout)
	}
}
