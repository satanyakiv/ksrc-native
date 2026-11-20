package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// ParseFileID keeps the inner path of a file-id below the root. Source.File
// is the second check, at the place where the file is opened: it must hold
// for an inner path that never went through the parser, and for a symbolic
// link that leaves the root.
func TestSourceFileStaysBelowTheRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	testutil.WriteFile(t, filepath.Join(root, "dir", "A.kt"), []byte("a\n"), 0o644)
	testutil.WriteFile(t, filepath.Join(base, "secret.txt"), []byte("secret\n"), 0o644)
	for link, dest := range map[string]string{
		filepath.Join(root, "inner.kt"):   filepath.Join("dir", "A.kt"),
		filepath.Join(root, "leak.txt"):   filepath.Join("..", "secret.txt"),
		filepath.Join(root, "up"):         "..",
		filepath.Join(base, "other-name"): "root",
	} {
		if err := os.Symlink(dest, link); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, "dir", "A.kt"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		root  string
		inner string
		want  string // empty: refused
	}{
		{"a file", root, "dir/A.kt", real},
		{"a path that goes up and stays inside", root, "dir/../dir/A.kt", real},
		{"a link to a file inside the root", root, "inner.kt", real},
		{"a root that is reached through a link", filepath.Join(base, "other-name"), "dir/A.kt", real},

		{"a parent directory", root, "../secret.txt", ""},
		{"parents after a directory", root, "dir/../../secret.txt", ""},
		{"a link to a file outside the root", root, "leak.txt", ""},
		{"a link to a directory outside the root", root, "up/secret.txt", ""},
		{"a file that does not exist", root, "dir/B.kt", ""},
		{"a directory", root, "dir", ""},
		{"the root itself", root, ".", ""},
		{"an empty path", root, "", ""},
		{"a root that does not exist", filepath.Join(base, "nope"), "dir/A.kt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Source{Root: tt.root}.File(tt.inner)
			if ok != (tt.want != "") || got != tt.want {
				t.Errorf("Source{Root: %s}.File(%q) = %q, %v; want %q", tt.root, tt.inner, got, ok, tt.want)
			}
		})
	}
}
