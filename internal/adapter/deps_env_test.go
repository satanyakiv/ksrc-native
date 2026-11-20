package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// KONAN_HOME names a distribution as plainly as --konan-home does. When it
// cannot be used, deps fails; a list of SDKs with exit code 0 would hide it.
func TestDepsFailsOnAKonanHomeVariableThatCannotBeUsed(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	nope := filepath.Join(t.TempDir(), "nope")
	w.env["KONAN_HOME"] = nope

	err := w.a.Deps(ctx, DepsRequest{})
	if errs.Code(err) != errs.NoKonan || !strings.Contains(err.Error(), "KONAN_HOME points at "+nope) {
		t.Errorf("Deps() with KONAN_HOME=%s: error = %v, want %s that names the variable and the path", nope, err, errs.NoKonan)
	}
	if w.stdout.Len() != 0 || strings.Contains(w.stderr.String(), "skipped") {
		t.Errorf("Deps() printed stdout %q and stderr %q, want nothing: the SDK list must not hide the failure", w.stdout.String(), w.stderr.String())
	}

	// A variable that points at a distribution selects it.
	w.env["KONAN_HOME"] = w.old.Home
	w.reset()
	if err := w.a.Deps(ctx, DepsRequest{}); err != nil {
		t.Fatalf("Deps() with KONAN_HOME=%s error = %v", w.old.Home, err)
	}
	if !strings.Contains(w.stdout.String(), "kn.platform:UserNotifications:2.4.10 (ios_arm64, dump cached: no)\n") || strings.Count(w.stdout.String(), "sdk.apple:<Framework>:") != 3 {
		t.Errorf("stdout = %q, want the 2.4.10 library and the three SDK lines", w.stdout.String())
	}

	// An empty variable is no variable: without a distribution the Kotlin/Native
	// part is skipped, as before.
	w.env["KONAN_HOME"] = ""
	w.a.UserHome = t.TempDir()
	w.reset()
	if err := w.a.Deps(ctx, DepsRequest{}); err != nil || strings.Count(w.stdout.String(), "sdk.apple:<Framework>:") != 3 {
		t.Errorf("Deps() without a distribution and without KONAN_HOME = %v, stdout = %q; want the three SDK lines", err, w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), "ksrc-native: skipped: E_NO_KONAN: ") {
		t.Errorf("stderr = %q, want the skipped note", w.stderr.String())
	}
}
