package provider

import (
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

func TestParseCoordinate(t *testing.T) {
	tests := []struct {
		in   string
		want Coordinate
	}{
		{"kn.platform:UIKit", Coordinate{Group: "kn.platform", Name: "UIKit"}},
		{"kn.platform:UIKit:2.4.21", Coordinate{Group: "kn.platform", Name: "UIKit", Version: "2.4.21"}},
		{" kn.platform:Foundation:2.5.0-RC2 ", Coordinate{Group: "kn.platform", Name: "Foundation", Version: "2.5.0-RC2"}},
	}
	for _, tt := range tests {
		got, err := ParseCoordinate(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseCoordinate(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}

	for _, in := range []string{"", "UIKit", "kn.platform:", ":UIKit", "a:b:c:d", "kn.platform:UI Kit", "kn.platform:UIKit:2.4.21!/x", "kn.platform:../x"} {
		if _, err := ParseCoordinate(in); errs.Code(err) != errs.BadCoord {
			t.Errorf("ParseCoordinate(%q) error = %v, want %s", in, err, errs.BadCoord)
		}
	}
}

func TestCoordinateString(t *testing.T) {
	if got := (Coordinate{Group: "kn.platform", Name: "UIKit"}).String(); got != "kn.platform:UIKit" {
		t.Errorf("String() = %q", got)
	}
	if got := (Coordinate{Group: "kn.platform", Name: "UIKit", Version: "2.4.21"}).String(); got != "kn.platform:UIKit:2.4.21" {
		t.Errorf("String() = %q", got)
	}
}

func TestParseFileID(t *testing.T) {
	tests := []struct {
		in        string
		wantCoord string
		wantInner string
	}{
		{"kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt", "kn.platform:UIKit:2.4.21", "ios_arm64/UIApplication.kt"},
		{"kn.platform:UIKit!/ios_arm64/_toplevel.kt", "kn.platform:UIKit", "ios_arm64/_toplevel.kt"},
		// A path that tries to leave the module root is folded back into it.
		{"kn.platform:UIKit:2.4.21!/../../etc/passwd", "kn.platform:UIKit:2.4.21", "etc/passwd"},
		{"kn.platform:UIKit:2.4.21!/ios_arm64/./a/../UIView.kt", "kn.platform:UIKit:2.4.21", "ios_arm64/UIView.kt"},
	}
	for _, tt := range tests {
		got, err := ParseFileID(tt.in)
		if err != nil {
			t.Errorf("ParseFileID(%q) error = %v", tt.in, err)
			continue
		}
		if got.Coordinate.String() != tt.wantCoord || got.Inner != tt.wantInner {
			t.Errorf("ParseFileID(%q) = %s + %s; want %s + %s", tt.in, got.Coordinate, got.Inner, tt.wantCoord, tt.wantInner)
		}
	}

	for _, in := range []string{"", "kn.platform:UIKit:2.4.21", "kn.platform:UIKit:2.4.21!/", "UIKit!/a.kt", "kn.platform:UIKit!/..", `kn.platform:UIKit!/a\b.kt`} {
		if _, err := ParseFileID(in); errs.Code(err) != errs.BadFileID {
			t.Errorf("ParseFileID(%q) error = %v, want %s", in, err, errs.BadFileID)
		}
	}
}

func TestFileIDRoundTrip(t *testing.T) {
	const id = "kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt"
	parsed, err := ParseFileID(id)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.String() != id {
		t.Errorf("String() = %q, want %q", parsed.String(), id)
	}

	src := Source{Coordinate: parsed.Coordinate}
	if got := src.FileID("ios_arm64/UIApplication.kt"); got != id {
		t.Errorf("Source.FileID() = %q, want %q", got, id)
	}
	if got := src.FileID(""); got != "kn.platform:UIKit:2.4.21!/" {
		t.Errorf("Source.FileID(\"\") = %q, want the prefix", got)
	}
}
