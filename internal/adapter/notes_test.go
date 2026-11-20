package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// docs/cli-api.md promises that every note on stderr starts with
// "ksrc-native: ", so a script can tell notes from the output of a program
// that shares the stream.
func TestEveryNoteOnStderrStartsWithTheToolName(t *testing.T) {
	w := newWorld(t)
	project := t.TempDir()
	testutil.WriteFile(t, filepath.Join(project, "gradle", "libs.versions.toml"), []byte("[versions]\nkotlin = \"2.9.9\"\n"), 0o644)

	// Three notes in one run: the project names a version that is not
	// installed, the dump is built for the first time, nothing matches.
	err := w.a.Search(context.Background(), SearchRequest{
		Common:  Common{Project: project},
		Pattern: "zzzNoSuchSymbol",
		Modules: []string{unModule},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	lines := strings.Split(strings.TrimRight(w.stderr.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stderr has %d lines, want 3 notes:\n%s", len(lines), w.stderr.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "ksrc-native: ") {
			t.Errorf("the note %q does not start with %q", line, "ksrc-native: ")
		}
	}
	if !strings.Contains(lines[0], "the project uses Kotlin 2.9.9") || !strings.Contains(lines[0], "using 2.4.21") {
		t.Errorf("the first note = %q, want the fallback from 2.9.9 to 2.4.21", lines[0])
	}
}
