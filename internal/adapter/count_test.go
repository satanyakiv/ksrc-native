package adapter

import (
	"context"
	"strings"
	"testing"
)

func TestCounted(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 entries"},
		{1, "1 entry"},
		{2, "2 entries"},
		{11, "11 entries"},
	}
	for _, tt := range tests {
		if got := counted(tt.n, "entry", "entries"); got != tt.want {
			t.Errorf("counted(%d, entry, entries) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// One cached dump is "1 entry" and "1 platform dump" in every line that
// counts: the total, the question, the deletion and the doctor line.
func TestOneCacheEntryIsCountedInTheSingular(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.a.Search(ctx, SearchRequest{Pattern: "x", Modules: []string{unModule}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	w.reset()
	if err := w.a.Doctor(ctx, DoctorRequest{}); err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if line := doctorLine(t, w.stdout.String(), "cache"); !strings.Contains(line, " (1 platform dump, ") {
		t.Errorf("Doctor() cache line = %q, want it to say %q", line, "1 platform dump")
	}

	w.reset()
	w.a.IsTerminal = func() bool { return true }
	w.a.Stdin = strings.NewReader("n\n")
	if err := w.a.Clean(ctx, CleanRequest{}); err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	for _, part := range []string{"total: 1 entry, ", "Delete 1 entry ("} {
		if !strings.Contains(w.stdout.String(), part) {
			t.Errorf("Clean() answered with n: stdout lacks %q:\n%s", part, w.stdout.String())
		}
	}

	w.reset()
	if err := w.a.Clean(ctx, CleanRequest{Yes: true}); err != nil {
		t.Fatalf("Clean(-y) error = %v", err)
	}
	if !strings.Contains(w.stdout.String(), "deleted 1 entry, ") {
		t.Errorf("Clean(-y) stdout = %q, want it to say %q", w.stdout.String(), "deleted 1 entry, ")
	}
}
