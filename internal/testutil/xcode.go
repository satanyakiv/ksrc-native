package testutil

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/executil"
)

// fakeBin is the directory Overlay pretends its fake programs live in.
const fakeBin = "/ksrc-native-fake-bin/"

// Overlay is an executil.Runner that fakes some programs and passes the rest
// to Base. Tests use it to keep the real rg and the fake klib script while
// replacing xcrun and xcode-select.
type Overlay struct {
	Base executil.Runner
	// Fakes maps a program name to its implementation.
	Fakes map[string]func(ctx context.Context, cmd executil.Command) error
	// Hidden lists programs that LookPath must not find, even if Base has
	// them.
	Hidden []string
}

// LookPath implements executil.Runner.
func (o *Overlay) LookPath(name string) (string, error) {
	for _, hidden := range o.Hidden {
		if hidden == name {
			return "", fmt.Errorf("%s: executable file not found in $PATH", name)
		}
	}
	if _, ok := o.Fakes[name]; ok {
		return fakeBin + name, nil
	}
	return o.Base.LookPath(name)
}

// Run implements executil.Runner.
func (o *Overlay) Run(ctx context.Context, cmd executil.Command) error {
	if name, ok := strings.CutPrefix(cmd.Path, fakeBin); ok {
		if fake, ok := o.Fakes[name]; ok {
			return fake(ctx, cmd)
		}
	}
	return o.Base.Run(ctx, cmd)
}

// Xcode is a fake Xcode developer directory with SDKs, plus fake xcrun and
// xcode-select programs that answer from it.
type Xcode struct {
	Developer string
	sdks      map[string]string // name accepted by `xcrun --sdk` -> path it prints
}

// NewXcode creates an empty developer directory.
func NewXcode(tb testing.TB) *Xcode {
	tb.Helper()
	developer := filepath.Join(tb.TempDir(), "Xcode.app", "Contents", "Developer")
	if err := os.MkdirAll(filepath.Join(developer, "Platforms"), 0o755); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	return &Xcode{Developer: developer, sdks: make(map[string]string)}
}

// AddSDK adds an SDK the way Xcode lays it out: a real directory
// "<Platform>.sdk" and a versioned symbolic link "<Platform><version>.sdk"
// to it. xcrun answers for the short name (iphoneos) and the canonical name
// (iphoneos26.4) with the versioned path. It returns the real directory.
func (x *Xcode) AddSDK(tb testing.TB, platform, version string) string {
	tb.Helper()
	sdks := filepath.Join(x.Developer, "Platforms", platform+".platform", "Developer", "SDKs")
	realDir := filepath.Join(sdks, platform+".sdk")
	short := strings.ToLower(platform)
	canonical := short + version
	settings := fmt.Sprintf(`{"CanonicalName":%q,"Version":%q,"DisplayName":"%s %s"}`, canonical, version, platform, version)
	WriteFile(tb, filepath.Join(realDir, "SDKSettings.json"), []byte(settings), 0o644)
	if err := os.MkdirAll(filepath.Join(realDir, "System", "Library", "Frameworks"), 0o755); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	versioned := filepath.Join(sdks, platform+version+".sdk")
	if err := os.Symlink(platform+".sdk", versioned); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	x.sdks[short] = versioned
	x.sdks[canonical] = versioned
	return realDir
}

// AddHeader adds a header to a framework of an SDK directory.
func (x *Xcode) AddHeader(tb testing.TB, sdkDir, framework, name, content string) {
	tb.Helper()
	WriteFile(tb, filepath.Join(sdkDir, "System", "Library", "Frameworks", framework+".framework", "Headers", name), []byte(content), 0o644)
}

// AddVersionedHeader adds a header the way macOS frameworks keep them: the
// file lives in Versions/A/Headers and Headers is a symbolic link.
func (x *Xcode) AddVersionedHeader(tb testing.TB, sdkDir, framework, name, content string) {
	tb.Helper()
	fw := filepath.Join(sdkDir, "System", "Library", "Frameworks", framework+".framework")
	WriteFile(tb, filepath.Join(fw, "Versions", "A", "Headers", name), []byte(content), 0o644)
	for link, dest := range map[string]string{
		filepath.Join(fw, "Versions", "Current"): "A",
		filepath.Join(fw, "Headers"):             filepath.Join("Versions", "Current", "Headers"),
	} {
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		if err := os.Symlink(dest, link); err != nil {
			tb.Fatalf("testutil: %v", err)
		}
	}
}

// AddSwiftOnlyFramework adds a framework without a Headers directory.
func (x *Xcode) AddSwiftOnlyFramework(tb testing.TB, sdkDir, framework string) {
	tb.Helper()
	WriteFile(tb, filepath.Join(sdkDir, "System", "Library", "Frameworks", framework+".framework", "Modules", "module.swiftinterface"), []byte("// swift\n"), 0o644)
}

// Fakes returns the fake xcrun and xcode-select for an Overlay.
func (x *Xcode) Fakes() map[string]func(context.Context, executil.Command) error {
	return map[string]func(context.Context, executil.Command) error{
		"xcrun":        x.xcrun,
		"xcode-select": x.xcodeSelect,
	}
}

func (x *Xcode) xcrun(_ context.Context, cmd executil.Command) error {
	// Supported: --sdk <name> --show-sdk-path
	if len(cmd.Args) != 3 || cmd.Args[0] != "--sdk" || cmd.Args[2] != "--show-sdk-path" {
		return &executil.ExitError{Path: cmd.Path, Code: 64, Stderr: "fake xcrun: unsupported arguments: " + strings.Join(cmd.Args, " ")}
	}
	path, ok := x.sdks[cmd.Args[1]]
	if !ok {
		return &executil.ExitError{Path: cmd.Path, Code: 1, Stderr: fmt.Sprintf(
			"xcodebuild: error: SDK %q cannot be located.\nxcrun: error: unable to lookup item 'Path' in SDK '%s'\n", cmd.Args[1], cmd.Args[1])}
	}
	_, err := io.WriteString(cmd.Stdout, path+"\n")
	return err
}

func (x *Xcode) xcodeSelect(_ context.Context, cmd executil.Command) error {
	if len(cmd.Args) != 1 || cmd.Args[0] != "-p" {
		return &executil.ExitError{Path: cmd.Path, Code: 64, Stderr: "fake xcode-select: unsupported arguments"}
	}
	_, err := io.WriteString(cmd.Stdout, x.Developer+"\n")
	return err
}

// InstallPrograms writes xcrun and xcode-select into dir as shell scripts
// that answer like the fakes of Fakes. A test that runs the built binary puts
// dir first on PATH: the binary starts real programs, so a fake in memory
// cannot stand in for them.
func (x *Xcode) InstallPrograms(tb testing.TB, dir string) {
	tb.Helper()
	var cases strings.Builder
	names := make([]string, 0, len(x.sdks))
	for name := range x.sdks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&cases, "  %s) echo '%s' ;;\n", name, x.sdks[name])
	}
	xcrun := "#!/bin/bash\n" +
		"if [ \"$#\" -ne 3 ] || [ \"$1\" != \"--sdk\" ] || [ \"$3\" != \"--show-sdk-path\" ]; then\n" +
		"  echo \"fake xcrun: unsupported arguments: $*\" >&2\n  exit 64\nfi\n" +
		"case \"$2\" in\n" + cases.String() +
		"  *) echo \"xcodebuild: error: SDK \\\"$2\\\" cannot be located.\" >&2; exit 1 ;;\n" +
		"esac\n"
	xcodeSelect := "#!/bin/bash\n" +
		"if [ \"$#\" -ne 1 ] || [ \"$1\" != \"-p\" ]; then\n" +
		"  echo \"fake xcode-select: unsupported arguments\" >&2\n  exit 64\nfi\n" +
		"echo '" + x.Developer + "'\n"
	WriteFile(tb, filepath.Join(dir, "xcrun"), []byte(xcrun), 0o755)
	WriteFile(tb, filepath.Join(dir, "xcode-select"), []byte(xcodeSelect), 0o755)
}

// StdlibUtils is a small stand-in for nativeMain/kotlinx/cinterop/Utils.kt.
const StdlibUtils = `package kotlinx.cinterop

public class MemScope

public inline fun <R> memScoped(block: MemScope.() -> R): R {
    val memScope = MemScope()
    return memScope.block()
}
`

// AddStdlibSources writes the sources zip of the native stdlib into the
// distribution. files maps archive paths to content; nil writes a default
// archive with StdlibUtils and one common file.
func (d Distribution) AddStdlibSources(tb testing.TB, files map[string]string) string {
	tb.Helper()
	if files == nil {
		files = map[string]string{
			"nativeMain/kotlinx/cinterop/Utils.kt":  StdlibUtils,
			"commonMain/kotlin/collections/Sets.kt": "package kotlin.collections\n\npublic fun <T> emptySet(): Set<T> = EmptySet\n",
		}
	}
	path := filepath.Join(d.Home, "sources", "kotlin-stdlib-native-sources.zip")
	WriteFile(tb, path, ZipBytes(tb, files), 0o644)
	return path
}

// ZipBytes builds a zip archive in memory. A name that ends with "/" becomes
// a directory entry.
func ZipBytes(tb testing.TB, files map[string]string) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	// Sorted, so the archive is the same on every run.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			tb.Fatalf("testutil: %v", err)
		}
		if !strings.HasSuffix(name, "/") {
			if _, err := io.WriteString(f, files[name]); err != nil {
				tb.Fatalf("testutil: %v", err)
			}
		}
	}
	if err := w.Close(); err != nil {
		tb.Fatalf("testutil: %v", err)
	}
	return buf.Bytes()
}
