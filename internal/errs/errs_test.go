package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorFormat(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"with hint", New(NoKonan, "no distribution found", "pass --konan-home"), "E_NO_KONAN: no distribution found. Try: pass --konan-home"},
		{"message ends with period", New(NoSources, "nothing here.", "search again"), "E_NO_SOURCES: nothing here. Try: search again"},
		{"no hint", New(BadLines, "bad range", ""), "E_BAD_LINES: bad range."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodeAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	err := fmt.Errorf("outer: %w", Wrap(cause, KlibFailed, "klib failed", ""))

	if got := Code(err); got != KlibFailed {
		t.Errorf("Code() = %q, want %q", got, KlibFailed)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is did not find the cause")
	}
	if got := Code(cause); got != "" {
		t.Errorf("Code(plain error) = %q, want empty", got)
	}
}
