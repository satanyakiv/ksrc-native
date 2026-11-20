package cat

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

func TestParseLines(t *testing.T) {
	tests := []struct {
		in   string
		want Range
	}{
		{"", Range{}},
		{"1,20", Range{Start: 1, End: 20}},
		{" 5 , 9 ", Range{Start: 5, End: 9}},
		{"7", Range{Start: 7, End: 7}},
		{"7,", Range{Start: 7}},
		{"3-4", Range{Start: 3, End: 4}},
		{"3:4", Range{Start: 3, End: 4}},
		{"4,4", Range{Start: 4, End: 4}},
	}
	for _, tt := range tests {
		got, err := ParseLines(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseLines(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}

	for _, in := range []string{"0,5", "-1,5", "5,2", "a,b", "1,2,3", ",5", "1,x"} {
		if _, err := ParseLines(in); errs.Code(err) != errs.BadLines {
			t.Errorf("ParseLines(%q) error = %v, want %s", in, err, errs.BadLines)
		}
	}
}

func TestPrint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "A.kt")
	if err := os.WriteFile(path, []byte("one\ntwo\r\nthree\n\nfive"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		r       Range
		numbers bool
		want    string
	}{
		{"whole file", Range{}, false, "one\ntwo\nthree\n\nfive\n"},
		{"inner range", Range{Start: 2, End: 3}, false, "two\nthree\n"},
		{"single line with number", Range{Start: 5, End: 5}, true, "5\tfive\n"},
		{"open end", Range{Start: 4}, true, "4\t\n5\tfive\n"},
		{"end past the file", Range{Start: 4, End: 99}, false, "\nfive\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Print(&out, path, tt.r, tt.numbers); err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			if out.String() != tt.want {
				t.Errorf("Print() = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestPrintErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "A.kt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := Print(&out, path, Range{Start: 10, End: 20}, false)
	if errs.Code(err) != errs.BadLines || !strings.Contains(err.Error(), "the file has 2 lines") {
		t.Errorf("Print(start past the end) error = %v, want %s naming the file length", err, errs.BadLines)
	}
	if out.Len() != 0 {
		t.Errorf("Print() wrote %q before failing", out.String())
	}

	if err := Print(&out, filepath.Join(t.TempDir(), "missing.kt"), Range{}, false); !os.IsNotExist(err) {
		t.Errorf("Print(missing file) error = %v, want not-exist", err)
	}
}
