package dump

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// dumpOf wraps the lines of fragments into a library block.
func dumpOf(lines ...string) string {
	return "library {\n  // module name: <test>\n\n" + strings.Join(lines, "\n") + "\n}\n"
}

// The splitter knows where a class or a package block ends by one exact
// line. If a later Kotlin version indents that line differently, the block
// never ends for the splitter, and every class after it would land in one
// file. A fragment marker inside an open block gives that away.
func TestSplitRejectsADumpWhoseBlocksDoNotClose(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // part of the message
	}{
		{
			"a fragment ends inside a class",
			dumpOf(
				"  library fragment {",
				"    // package name: platform.Changed",
				"",
				"    public final class platform/Changed/First : kotlin/Any {",
				"      public final fun one(): kotlin/Unit",
				"   }", // the closing line moved by one space
				"  }",
				"  library fragment {",
				"    public final class platform/Changed/Second : kotlin/Any {",
				"    }",
				"  }",
			),
			"1 time",
		},
		{
			"a fragment starts inside a package block",
			dumpOf(
				"  library fragment {",
				"    package {",
				"      public typealias A = kotlin/Int",
				"  library fragment {",
				"    package {",
				"      public typealias B = kotlin/Int",
				"    }",
				"  }",
			),
			"1 time",
		},
		{
			"every class of two fragments stays open",
			dumpOf(
				"  library fragment {",
				"    public final class platform/Changed/First : kotlin/Any {",
				"  }",
				"  library fragment {",
				"    public final class platform/Changed/Second : kotlin/Any {",
				"  }",
			),
			"2 times",
		},
		{
			"the dump ends inside a class",
			"library {\n  library fragment {\n    public final class platform/Cut/Short : kotlin/Any {\n      public final fun one(): kotlin/Unit\n",
			"1 time",
		},
		{
			"the dump ends inside a fragment",
			"library {\n  library fragment {\n    // package name: platform.Cut\n",
			"1 time",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Split(strings.NewReader(tt.input))
			if errs.Code(err) != errs.DumpFormat || result != nil {
				t.Fatalf("Split() = %+v, %v; want no result and %s", result, err, errs.DumpFormat)
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "ksrc-native doctor") {
				t.Errorf("Split() error = %v, want one that says %q and names `ksrc-native doctor`", err, tt.want)
			}
		})
	}
}

// A balanced dump with the same shapes still splits.
func TestSplitAcceptsBalancedBlocks(t *testing.T) {
	input := dumpOf(
		"  library fragment {",
		"    public final class platform/Fine/First : kotlin/Any {",
		"      public final fun one(): kotlin/Unit",
		"    }",
		"  }",
		"  library fragment {",
		"    package {",
		"      public typealias A = kotlin/Int",
		"    }",
		"  }",
	)
	result, err := Split(strings.NewReader(input))
	if err != nil || len(result.Files) != 2 {
		t.Fatalf("Split() = %+v, %v; want two files", result, err)
	}
}

func TestLongClassNameGetsAShortFileName(t *testing.T) {
	long := strings.Repeat("A", 300)
	other := strings.Repeat("A", 299) + "B" // the same first 200 characters
	input := dumpOf(
		"  library fragment {",
		"    public final class platform/Long/"+long+" : kotlin/Any {",
		"      public final fun one(): kotlin/Unit",
		"    }",
		"    public final companion object platform/Long/"+long+".Companion : kotlin/Any {",
		"    }",
		"    public final class platform/Long/"+other+" : kotlin/Any {",
		"    }",
		"    public final class platform/Long/Short : kotlin/Any {",
		"    }",
		"  }",
	)
	result, err := Split(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if len(result.Files) != 3 {
		t.Fatalf("Split() made %d files, want 3: the long class with its companion, the other long class, the short one", len(result.Files))
	}

	shape := regexp.MustCompile(`^A{200}-[0-9a-f]{8}\.kt$`)
	first, second := result.Files[0], result.Files[1]
	for _, f := range []File{first, second} {
		if !shape.MatchString(f.Name) {
			t.Errorf("file name %q (%d bytes), want 200 characters of the class name, a dash, 8 hex digits and .kt", f.Name, len(f.Name))
		}
	}
	if first.Name == second.Name {
		t.Errorf("two class names with the same first 200 characters share the file %q", first.Name)
	}
	if result.Files[2].Name != "Short.kt" {
		t.Errorf("a short class name became %q, want Short.kt", result.Files[2].Name)
	}
	// The full name stays in the content, so a search for it still hits.
	content := string(first.Content)
	if !strings.Contains(content, "class platform/Long/"+long+" :") || !strings.Contains(content, "object platform/Long/"+long+".Companion :") {
		t.Errorf("the file of the long class lacks the full name of the class or of its companion:\n%.200s", content)
	}

	// 255 bytes is the longest file name on APFS and ext4.
	dir := t.TempDir()
	if err := result.WriteTo(dir); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	for _, f := range result.Files {
		if data, err := os.ReadFile(filepath.Join(dir, f.Name)); err != nil || string(data) != string(f.Content) {
			t.Errorf("the written file %s reads back wrong: %v", f.Name, err)
		}
	}

	// The name does not depend on the run.
	again, err := Split(strings.NewReader(input))
	if err != nil || again.Files[0].Name != first.Name {
		t.Errorf("a second Split() named the file %q, want %q again", again.Files[0].Name, first.Name)
	}
}
