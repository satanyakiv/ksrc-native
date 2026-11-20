package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/adapter"
	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

// groundTruthVersion is the Kotlin version the declarations below were
// collected from with `klib dump-metadata` (target ios_arm64).
const groundTruthVersion = "2.4.21"

// TestRealDistribution runs the tool against a Kotlin/Native distribution in
// ~/.konan with the real klib tool and an empty cache. It needs a JDK and
// takes about ten seconds, so it is skipped with -short and when no
// distribution is installed.
func TestRealDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped with -short: dumps real platform klibs")
	}
	testutil.RequireRipgrep(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dist, err := konan.Discover(konan.DiscoverOptions{Getenv: os.Getenv, UserHome: home})
	if err != nil {
		t.Skipf("no Kotlin/Native distribution: %v", err)
	}
	// Prefer the version the ground truth was collected from.
	if d, err := konan.Discover(konan.DiscoverOptions{Getenv: os.Getenv, UserHome: home, KotlinVersion: groundTruthVersion}); err == nil {
		dist = d
	}
	if len(dist.Frameworks(konan.DefaultTarget)) == 0 {
		t.Skipf("the distribution at %s has no %s platform libraries", dist.Home, konan.DefaultTarget)
	}
	exact := dist.Version == groundTruthVersion
	t.Logf("distribution: %s at %s (exact declarations checked: %v)", dist.Version, dist.Home, exact)

	cacheDir := filepath.Join(t.TempDir(), "cache")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	a := &adapter.Adapter{
		Runner: executil.OS{},
		Stdout: out,
		Stderr: errOut,
		Getenv: func(key string) string {
			if key == cache.EnvDir {
				return cacheDir
			}
			return os.Getenv(key)
		},
		UserHome:   home,
		Stdin:      strings.NewReader(""),
		IsTerminal: func() bool { return false },
		GOOS:       "darwin",
		GOARCH:     "arm64",
	}
	run := func(args ...string) (int, string, string) {
		out.Reset()
		errOut.Reset()
		args = append([]string{"--konan-home", dist.Home, "--project", t.TempDir()}, args...)
		code := Run(context.Background(), args, a)
		return code, out.String(), errOut.String()
	}

	tests := []struct {
		symbol      string
		framework   string
		pattern     string
		file        string
		declaration string
	}{
		{
			symbol: "NSURL", framework: "Foundation",
			pattern:     `class platform/Foundation/NSURL `,
			file:        "NSURL.kt",
			declaration: "public open class platform/Foundation/NSURL : platform/darwin/NSObject, platform/Foundation/NSSecureCodingProtocol, platform/Foundation/NSCopyingProtocol",
		},
		{
			symbol: "UIApplication", framework: "UIKit",
			pattern:     `class platform/UIKit/UIApplication `,
			file:        "UIApplication.kt",
			declaration: "public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder",
		},
		{
			symbol: "WKNavigationDelegateProtocol", framework: "WebKit",
			pattern:     `interface platform/WebKit/WKNavigationDelegateProtocol `,
			file:        "WKNavigationDelegateProtocol.kt",
			declaration: "public abstract interface platform/WebKit/WKNavigationDelegateProtocol : platform/darwin/NSObjectProtocol",
		},
		{
			symbol: "UNUserNotificationCenter", framework: "UserNotifications",
			pattern:     `class platform/UserNotifications/UNUserNotificationCenter `,
			file:        "UNUserNotificationCenter.kt",
			declaration: "public open class platform/UserNotifications/UNUserNotificationCenter : platform/darwin/NSObject",
		},
		{
			symbol: "NSURLErrorDomain", framework: "Foundation",
			pattern:     `val NSURLErrorDomain:`,
			file:        "_toplevel.kt",
			declaration: "public final val NSURLErrorDomain: kotlin/String? /* = platform/Foundation/NSErrorDomain^? */",
		},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			code, stdout, stderr := run("search", tt.pattern, "--module", "kn.platform:"+tt.framework)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
			}
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			if len(lines) != 1 || lines[0] == "" {
				t.Fatalf("got %d lines, want exactly one:\n%s", len(lines), stdout)
			}
			wantID := "kn.platform:" + tt.framework + ":" + dist.Version + "!/" + konan.DefaultTarget + "/" + tt.file
			fileID, rest, _ := strings.Cut(lines[0], " ")
			if fileID != wantID {
				t.Errorf("file-id = %q, want %q", fileID, wantID)
			}
			// rest is "<line>:<col>:<text>".
			parts := strings.SplitN(rest, ":", 3)
			if len(parts) != 3 {
				t.Fatalf("line %q does not follow <file-id> <line>:<col>:<text>", lines[0])
			}
			got := strings.TrimSuffix(parts[2], " {")
			if exact && got != tt.declaration {
				t.Errorf("declaration =\n  %s\nwant\n  %s", got, tt.declaration)
			}
			if !exact && !strings.Contains(got, tt.symbol) {
				t.Errorf("declaration %q does not name %s", got, tt.symbol)
			}

			// cat of the reported line prints the same declaration.
			code, stdout, stderr = run("cat", fileID, "--lines", parts[0])
			if code != 0 || strings.TrimSuffix(strings.TrimSpace(stdout), " {") != got {
				t.Errorf("cat --lines %s: code = %d, stdout = %q, stderr = %q", parts[0], code, stdout, stderr)
			}
		})
	}

	// Everything above is cached now: a search without --module covers all
	// four libraries and does not run klib again.
	code, stdout, stderr := run("search", `^public open class platform/\w+/(NSURL|UIApplication|UNUserNotificationCenter) `)
	if code != 0 {
		t.Fatalf("search without --module: exit code = %d, stderr:\n%s", code, stderr)
	}
	if n := strings.Count(stdout, "\n"); n != 3 {
		t.Errorf("search without --module found %d lines, want 3:\n%s", n, stdout)
	}
	if strings.Contains(stderr, "dumping") {
		t.Errorf("a search over cached dumps rebuilt one: %s", stderr)
	}

	code, stdout, _ = run("doctor")
	if code != 0 || !strings.Contains(stdout, "(4 platform dumps, ") {
		t.Errorf("doctor: code = %d, output:\n%s", code, stdout)
	}

	t.Run("stdlib", func(t *testing.T) {
		if _, err := os.Stat(dist.StdlibSources()); err != nil {
			t.Skipf("the distribution has no stdlib sources: %v", err)
		}
		code, stdout, stderr := run("search", `fun <R> memScoped`, "--module", "kn.stdlib:native")
		want := "kn.stdlib:native:" + dist.Version + "!/nativeMain/kotlinx/cinterop/Utils.kt "
		if code != 0 || !strings.HasPrefix(stdout, want) {
			t.Fatalf("code = %d, stdout = %q, stderr = %q; want a hit in %s", code, stdout, stderr, want)
		}
		fileID, rest, _ := strings.Cut(strings.SplitN(stdout, "\n", 2)[0], " ")
		line, _, _ := strings.Cut(rest, ":")
		code, stdout, _ = run("cat", fileID, "--lines", line)
		if code != 0 || !strings.Contains(stdout, "memScoped") {
			t.Errorf("cat --lines %s: code = %d, stdout = %q", line, code, stdout)
		}
	})

	t.Run("apple headers", func(t *testing.T) {
		if code, _, stderr := run("where", "sdk.apple:UIKit"); code != 0 {
			t.Skipf("no iphoneos SDK with UIKit headers: %s", strings.TrimSpace(stderr))
		}
		code, stdout, stderr := run("search", "presentViewController:", "--module", "sdk.apple:UIKit", "--", "-F")
		if code != 0 || !strings.HasPrefix(stdout, "sdk.apple:UIKit:iphoneos") || !strings.Contains(stdout, "!/UIViewController.h ") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q; want a hit in UIViewController.h", code, stdout, stderr)
		}
		fileID, _, _ := strings.Cut(stdout, " ")
		code, stdout, _ = run("cat", fileID, "--lines", "1,5")
		if code != 0 || stdout == "" {
			t.Errorf("cat of %s: code = %d, stdout = %q", fileID, code, stdout)
		}
	})
}
