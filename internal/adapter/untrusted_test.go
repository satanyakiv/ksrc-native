package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// The tests in this file treat the rg arguments, the target and the SDK as
// values that a model filled in after reading a hostile repository.

// trap is a script that leaves a marker when something runs it, and a file
// outside every module with a line worth stealing.
type trap struct {
	script  string
	marker  string
	outside string
}

func newTrap(t *testing.T) trap {
	t.Helper()
	dir := t.TempDir()
	tr := trap{
		script:  filepath.Join(dir, "pre.sh"),
		marker:  filepath.Join(dir, "marker"),
		outside: filepath.Join(dir, "outside.txt"),
	}
	testutil.WriteFile(t, tr.script, []byte("#!/bin/sh\ntouch '"+tr.marker+"'\ncat\n"), 0o755)
	testutil.WriteFile(t, tr.outside, []byte("hello-from-outside\n"), 0o644)
	return tr
}

func (tr trap) sprung() bool {
	_, err := os.Stat(tr.marker)
	return err == nil
}

func TestSearchRejectsRgArgsOutsideTheAllowList(t *testing.T) {
	w := newWorld(t)
	tr := newTrap(t)
	tests := []struct {
		name    string
		pattern string
		rgArgs  []string
		part    string
	}{
		{"a preprocessor", "presentViewController", []string{"--pre", tr.script}, `"--pre"`},
		{"a preprocessor with =", "presentViewController", []string{"--pre=" + tr.script}, "--pre="},
		{"a path outside the modules", "hello", []string{tr.outside}, "a path cannot be added"},
		{"patterns from a file", "x", []string{"-f", tr.outside}, `"-f"`},
		{"files with matches", "x", []string{"-l"}, `"-l"`},
		{"list files", "x", []string{"--files"}, `"--files"`},
		{"follow links", "x", []string{"-L"}, `"-L"`},
		{"a second --", "hello", []string{"--", tr.outside}, `"--"`},
		{"glob without a value", "x", []string{"-g"}, "-g needs a value"},
		{"max-count that is not a number", "x", []string{"--max-count", "all"}, `"all"`},
	}
	for _, module := range []string{"sdk.apple:UIKit", unModule} {
		for _, tt := range tests {
			t.Run(module+"/"+tt.name, func(t *testing.T) {
				w.reset()
				err := w.a.Search(context.Background(), SearchRequest{Pattern: tt.pattern, Modules: []string{module}, RgArgs: tt.rgArgs})
				if errs.Code(err) != errs.BadArgs {
					t.Fatalf("Search(%q, rg args %q) error = %v, want %s", tt.pattern, tt.rgArgs, err, errs.BadArgs)
				}
				if !strings.Contains(err.Error(), tt.part) || !strings.Contains(err.Error(), "Try: these rg arguments are allowed: -i ") {
					t.Errorf("error %q does not contain %q and the list of allowed arguments", err.Error(), tt.part)
				}
				if w.stdout.Len() != 0 {
					t.Errorf("a refused search wrote %q to stdout", w.stdout.String())
				}
			})
		}
	}
	if tr.sprung() {
		t.Error("a refused search ran the preprocessor script")
	}
	// The check comes before the expensive part.
	if calls := w.dist.KlibCalls(t); len(calls) != 0 {
		t.Errorf("a refused search built %d dumps, want 0", len(calls))
	}
}

func TestSearchAcceptsTheAllowedRgArgs(t *testing.T) {
	w := newWorld(t)
	const delegate = "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNUserNotificationCenterDelegateProtocol.kt "
	tests := []struct {
		name    string
		req     SearchRequest
		lines   int
		prefix  string // of the first line
		content string // somewhere in the output
	}{
		{"ignore case", SearchRequest{Pattern: "VAL UNERRORDOMAIN", RgArgs: []string{"-i"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:", "UNErrorDomain"},
		{"fixed string", SearchRequest{Pattern: "ObjCClassOf<platform/UserNotifications/UNNotification> {", RgArgs: []string{"-F"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt ", "companion object"},
		{"glob", SearchRequest{Pattern: "^public ", RgArgs: []string{"-g", "*Delegate*", "--max-count", "1"}}, 2, delegate, "UNUserNotificationCenterDelegateProtocol"},
		{"PCRE2", SearchRequest{Pattern: `val UNErrorDomain(?=: kotlin/String)`, RgArgs: []string{"-P"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/_toplevel.kt 14:14:", "UNErrorDomain"},
		{"max count", SearchRequest{Pattern: "platform/UserNotifications", RgArgs: []string{"--max-count", "1", "-g", "UNNotification.kt"}}, 1, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt ", ""},
		{"context and a glob", SearchRequest{Pattern: "class platform/UserNotifications/UNNotification ", Context: 1, RgArgs: []string{"-g", "UNNotification.kt"}}, 3, "kn.platform:UserNotifications:2.4.21!/ios_arm64/UNNotification.kt 1-", "2:13:public open class"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			tt.req.Modules = []string{unModule}
			if err := w.a.Search(context.Background(), tt.req); err != nil {
				if tt.name == "PCRE2" && strings.Contains(err.Error(), "PCRE2") {
					t.Skip("the installed rg is built without PCRE2")
				}
				t.Fatalf("Search(%q, rg args %q) error = %v", tt.req.Pattern, tt.req.RgArgs, err)
			}
			lines := strings.Split(strings.TrimSpace(w.stdout.String()), "\n")
			if len(lines) != tt.lines || !strings.HasPrefix(lines[0], tt.prefix) || !strings.Contains(w.stdout.String(), tt.content) {
				t.Errorf("stdout =\n%swant %d lines, the first with prefix %q, and %q somewhere", w.stdout.String(), tt.lines, tt.prefix, tt.content)
			}
		})
	}
}

// A pattern is never an argument of rg, whatever it looks like.
func TestSearchTakesAFlagLikePatternAsAPattern(t *testing.T) {
	w := newWorld(t)
	tr := newTrap(t)
	for _, pattern := range []string{"--files", "--pre=" + tr.script, "-l", "--no-json", "--"} {
		w.reset()
		err := w.a.Search(context.Background(), SearchRequest{Pattern: pattern, Modules: []string{"sdk.apple:UIKit"}, RgArgs: []string{"-F"}})
		if err != nil {
			t.Errorf("Search(%q) error = %v, want a plain search without matches", pattern, err)
		}
		if w.stdout.Len() != 0 || !strings.Contains(w.stderr.String(), "no matches for ") {
			t.Errorf("Search(%q): stdout = %q, stderr = %q; want nothing and a no-matches note", pattern, w.stdout.String(), w.stderr.String())
		}
	}
	if tr.sprung() {
		t.Error("a pattern that names a preprocessor made rg run it")
	}
}
