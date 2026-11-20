package dump

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

// TestSplitGolden splits a real excerpt of the UserNotifications dump (an
// interface, classes, a companion object, an enum with nested classes, type
// aliases and top-level declarations spread over two fragments) and compares
// every produced file with its golden copy.
func TestSplitGolden(t *testing.T) {
	input := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	result, err := Split(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	goldenDir := testutil.FixturePath(t, "golden/usernotifications")
	if *update {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatal(err)
		}
		if err := result.WriteTo(goldenDir); err != nil {
			t.Fatal(err)
		}
	}

	wantNames := []string{
		"UNUserNotificationCenterDelegateProtocolMeta.kt",
		"UNUserNotificationCenterDelegateProtocol.kt",
		"UNNotificationMeta.kt",
		"UNNotification.kt",
		"UNShowPreviewsSetting.kt",
		"_toplevel.kt",
	}
	var gotNames []string
	for _, f := range result.Files {
		gotNames = append(gotNames, f.Name)
	}
	if strings.Join(gotNames, " ") != strings.Join(wantNames, " ") {
		t.Fatalf("files = %v, want %v", gotNames, wantNames)
	}

	for _, f := range result.Files {
		want, err := os.ReadFile(filepath.Join(goldenDir, f.Name))
		if err != nil {
			t.Fatalf("read golden (run `go test ./internal/dump -update` to create it): %v", err)
		}
		if !bytes.Equal(f.Content, want) {
			t.Errorf("%s differs from its golden file\n--- got ---\n%s\n--- want ---\n%s", f.Name, f.Content, want)
		}
	}

	// No stray golden files.
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, e := range entries {
		onDisk = append(onDisk, e.Name())
	}
	sort.Strings(onDisk)
	sort.Strings(wantNames)
	if strings.Join(onDisk, " ") != strings.Join(wantNames, " ") {
		t.Errorf("golden directory holds %v, want %v", onDisk, wantNames)
	}

	if result.Module != "org.jetbrains.kotlin.native.platform.UserNotifications" {
		t.Errorf("Module = %q", result.Module)
	}
	if got, want := strings.Join(result.Packages, " "), "platform platform.UserNotifications"; got != want {
		t.Errorf("Packages = %q, want %q", got, want)
	}
	// 2 interfaces, 2 classes, 1 companion, 1 enum with 3 nested declarations.
	if result.Classes != 9 {
		t.Errorf("Classes = %d, want 9", result.Classes)
	}
}

// TestSplitKeepsEveryDeclarationLine checks the property the tool relies on:
// every non-blank line of the dump that is not structure ends up in exactly
// one file.
func TestSplitKeepsEveryDeclarationLine(t *testing.T) {
	input := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	result, err := Split(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	want := contentLines(string(input))
	got := 0
	for _, f := range result.Files {
		for _, line := range strings.Split(string(f.Content), "\n") {
			if strings.TrimSpace(line) != "" {
				got++
			}
		}
	}
	if got != want {
		t.Errorf("split files hold %d non-blank lines, the dump has %d declaration lines", got, want)
	}
}

// contentLines counts the dump lines that Split must keep: everything except
// blank lines, the library and fragment braces, the name markers and the
// braces of `package { }` blocks.
func contentLines(dumpText string) int {
	count := 0
	inPackage := false
	for _, line := range strings.Split(dumpText, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case line == "library {", line == "}", line == fragmentOpen, line == fragmentClose:
		case strings.HasPrefix(line, "  "+moduleMarker), strings.HasPrefix(line, packageMarker), strings.HasPrefix(line, classMarker):
		case line == packageOpen:
			inPackage = true
		case line == blockClose && inPackage:
			inPackage = false
		default:
			count++
		}
	}
	return count
}

func TestSplitShiftsDeclarationsToColumnOne(t *testing.T) {
	input := testutil.ReadFixture(t, testutil.UserNotificationsDump)
	result, err := Split(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	files := make(map[string]string)
	for _, f := range result.Files {
		files[f.Name] = string(f.Content)
	}

	class := files["UNNotification.kt"]
	if !strings.HasPrefix(class, "@kotlinx/cinterop/ExternalObjCClass\npublic open class platform/UserNotifications/UNNotification : ") {
		t.Errorf("UNNotification.kt does not start with the annotation and the class header:\n%.200s", class)
	}
	if !strings.Contains(class, "\n  public final val date: platform/Foundation/NSDate\n") {
		t.Error("UNNotification.kt: members are not at two spaces of indent")
	}
	if !strings.Contains(class, "\npublic final companion object platform/UserNotifications/UNNotification.Companion : ") {
		t.Error("UNNotification.kt does not hold the companion object")
	}

	top := files[TopLevelFile]
	if !strings.HasPrefix(top, "public typealias UNNotificationSoundNameVar = ") {
		t.Errorf("_toplevel.kt does not start with the first type alias:\n%.200s", top)
	}
	if !strings.Contains(top, "\npublic final val UNErrorDomain: kotlin/String?\n  @kotlinx/cinterop/internal/CCall(") {
		t.Error("_toplevel.kt: a property and its accessor are not at columns one and three")
	}
	for name, content := range files {
		if strings.HasPrefix(content, "\n") || strings.HasSuffix(content, "\n\n") || strings.Contains(content, "\n\n\n") {
			t.Errorf("%s has a leading, trailing or doubled blank line", name)
		}
		if !strings.HasSuffix(content, "\n") {
			t.Errorf("%s does not end with a newline", name)
		}
	}
}

func TestSplitRejectsTextThatIsNotADump(t *testing.T) {
	for _, input := range []string{"", "error: library not found\n", "library {\n}\n"} {
		_, err := Split(strings.NewReader(input))
		if errs.Code(err) != errs.DumpFormat {
			t.Errorf("Split(%q) error = %v, want %s", input, err, errs.DumpFormat)
		}
	}
}

func TestSplitEdgeCases(t *testing.T) {
	input := strings.Join([]string{
		"library {",
		"  // module name: <demo>",
		"",
		"  library fragment {",
		"    // package name: demo",
		"",
		"    public final class demo/Box<T#0 /* T */> : kotlin/Any {",
		"",
		"      public constructor(value: T#0)",
		"    }",
		"",
		"    public final class demo/BOX {",
		"    }",
		"",
		"    @demo/Marker",
		"    public final object demo/Single",
		"",
		"    something unexpected",
		"",
		"    @demo/Dangling",
		"  }",
		"}",
		"",
	}, "\n")

	result, err := Split(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	files := make(map[string]string)
	for _, f := range result.Files {
		files[f.Name] = string(f.Content)
	}

	// Type parameters are not part of the file name, and names that differ
	// only by case share the first file.
	box := files["Box.kt"]
	if !strings.Contains(box, "public final class demo/Box<T#0 /* T */> : kotlin/Any {") || !strings.Contains(box, "public final class demo/BOX {") {
		t.Errorf("Box.kt = %q, want both Box and BOX", box)
	}
	if _, ok := files["BOX.kt"]; ok {
		t.Error("BOX.kt exists, want it merged into Box.kt")
	}

	// A declaration without a body keeps its annotation and does not swallow
	// the lines after it.
	if got, want := files["Single.kt"], "@demo/Marker\npublic final object demo/Single\n"; got != want {
		t.Errorf("Single.kt = %q, want %q", got, want)
	}

	// Lines of an unknown shape stay searchable in the top-level file.
	top := files[TopLevelFile]
	if !strings.Contains(top, "something unexpected\n") || !strings.Contains(top, "@demo/Dangling\n") {
		t.Errorf("_toplevel.kt = %q, want the unexpected line and the dangling annotation", top)
	}
	if result.Classes != 3 {
		t.Errorf("Classes = %d, want 3", result.Classes)
	}
}

func TestClassFileName(t *testing.T) {
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{"    public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {", "UIApplication.kt", true},
		{"    public abstract interface platform/WebKit/WKNavigationDelegateProtocol : platform/darwin/NSObjectProtocol {", "WKNavigationDelegateProtocol.kt", true},
		{"    public final companion object platform/UIKit/UIView.Companion : platform/UIKit/UIViewMeta {", "UIView.kt", true},
		{"    public final enum class platform/UserNotifications/UNShowPreviewsSetting : kotlin/Enum<platform/UserNotifications/UNShowPreviewsSetting> {", "UNShowPreviewsSetting.kt", true},
		{"    public final class platform/UserNotifications/UNShowPreviewsSetting.Var.Companion {", "UNShowPreviewsSetting.kt", true},
		{"    public final class TopLevelWithoutPackage {", "TopLevelWithoutPackage.kt", true},
		{"    public final fun notAClass(): kotlin/Unit", "", false},
	}
	for _, tt := range tests {
		got, ok := classFileName(tt.header)
		if got != tt.want || ok != tt.ok {
			t.Errorf("classFileName(%q) = %q, %v; want %q, %v", tt.header, got, ok, tt.want, tt.ok)
		}
	}
}

func TestWriteTo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ios_arm64")
	result := &Result{Files: []File{{Name: "A.kt", Content: []byte("a\n")}, {Name: TopLevelFile, Content: []byte("t\n")}}}
	if err := result.WriteTo(dir); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	for _, f := range result.Files {
		got, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil || !bytes.Equal(got, f.Content) {
			t.Errorf("%s = %q, %v; want %q", f.Name, got, err, f.Content)
		}
	}
}
