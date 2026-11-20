package adapter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/provider"
	"github.com/satanyakiv/ksrc-native/internal/search"
	"github.com/satanyakiv/ksrc-native/internal/xcode"
)

// DoctorFailed is the code Doctor returns when a check fails.
const DoctorFailed = "E_DOCTOR"

const javaHint = "kn.platform needs a JDK to run the klib tool: install one (macOS: brew install openjdk) or point JAVA_HOME at one"

// DoctorRequest is the input of Doctor.
type DoctorRequest struct {
	Common
}

// Doctor checks the environment and prints one line per check:
//
//	<name>: ok - <detail>
//	<name>: MISSING - <what to do>        the tool cannot work
//	<name>: unavailable - <what to do>    one optional source is off
//
// rg, the Kotlin/Native distribution, its klib tool, a java that runs, the
// platform libraries and a cache that can be written are required. The
// stdlib sources and Xcode are optional: without them kn.stdlib and
// sdk.apple do not work, the rest does.
//
// A check answers the question the operation will ask. "The file exists" is
// not enough for java (macOS ships a stub that fails without a JDK) and not
// enough for the cache (a file can sit where the directory should be).
func (a *Adapter) Doctor(ctx context.Context, req DoctorRequest) error {
	failed := 0
	report := func(name string, ok bool, detail string) {
		status := "ok"
		if !ok {
			status = "MISSING"
			failed++
		}
		fmt.Fprintf(a.Stdout, "%s: %s - %s\n", name, status, detail)
	}
	optional := func(name string, ok bool, detail string) {
		status := "ok"
		if !ok {
			status = "unavailable"
		}
		fmt.Fprintf(a.Stdout, "%s: %s - %s\n", name, status, detail)
	}

	// ripgrep
	if rg, err := search.FindRipgrep(a.runner()); err != nil {
		report("rg", false, err.Error())
	} else {
		report("rg", true, fmt.Sprintf("%s (%s)", rg, a.rgVersion(ctx, rg)))
	}

	store, storeErr := cache.Open(a.Getenv)

	// Kotlin/Native distribution, klib tool, platform libraries, stdlib
	r, err := a.newResolver(req.Common)
	if err != nil {
		return coded(err)
	}
	dist, err := r.distribution()
	if err != nil {
		report("kotlin-native", false, err.Error())
		report("klib", false, "needs a Kotlin/Native distribution")
		// Only klib runs java; without a distribution nothing needs it.
		optional("java", false, "needs a Kotlin/Native distribution")
		report("platform", false, "needs a Kotlin/Native distribution")
		optional("stdlib", false, "needs a Kotlin/Native distribution")
	} else {
		detail := fmt.Sprintf("%s at %s", dist.Version, dist.Home)
		if others := otherVersions(konan.Installed(konan.DataDir(a.Getenv, a.UserHome)), dist); others != "" {
			detail += " (also installed: " + others + ")"
		}
		report("kotlin-native", true, detail)

		tool := dist.KlibTool()
		if info, err := os.Stat(tool); err != nil || info.IsDir() {
			report("klib", false, "no klib tool at "+tool+"; delete the distribution and run a Kotlin/Native Gradle task once")
		} else {
			report("klib", true, tool)
		}

		// klib is a shell script that runs a JVM.
		if java, err := konan.FindJava(a.Getenv, a.runner()); err != nil {
			report("java", false, err.Error()+"; "+javaHint)
		} else if version, err := konan.JavaVersion(ctx, a.runner(), java); err != nil {
			report("java", false, err.Error()+"; "+javaHint)
		} else {
			report("java", true, fmt.Sprintf("%s (%s)", java, version))
		}

		target := req.target()
		if frameworks := dist.Frameworks(target); dist.RequireTarget(target) != nil {
			report("platform", false, fmt.Sprintf("no platform libraries for target %s; available targets: %s", target, strings.Join(dist.Targets(), ", ")))
		} else {
			report("platform", true, fmt.Sprintf("%d libraries for %s (%d targets in the distribution)", len(frameworks), target, len(dist.Targets())))
		}

		stdlib := &provider.Stdlib{Dist: dist, Cache: store}
		if !stdlib.Available() {
			optional("stdlib", false, "no sources zip at "+dist.StdlibSources()+"; kn.stdlib:native cannot be searched")
		} else {
			optional("stdlib", true, fmt.Sprintf("%s (extracted: %s)", dist.StdlibSources(), yesNo(storeErr == nil && stdlib.Cached())))
		}
	}

	// Xcode, for sdk.apple
	if sdk, err := xcode.Find(ctx, a.runner(), req.sdk()); err != nil {
		optional("xcode", false, err.Error())
	} else {
		optional("xcode", true, fmt.Sprintf("SDK %s at %s (%d frameworks with headers)", sdk.Name, sdk.Path, len(sdk.Frameworks())))
	}

	// cache
	if storeErr != nil {
		report("cache", false, storeErr.Error())
	} else if err := store.Check(); err != nil {
		report("cache", false, err.Error())
	} else {
		size, _ := store.Size()
		dumps := len(store.List(provider.PlatformNamespace))
		report("cache", true, fmt.Sprintf("%s (%d platform dumps, %s)", store.Root, dumps, humanSize(size)))
	}

	if failed > 0 {
		return errs.New(DoctorFailed, fmt.Sprintf("%d of the checks above failed", failed), "follow the hints in the MISSING lines")
	}
	return nil
}

func (a *Adapter) rgVersion(ctx context.Context, rg string) string {
	var out bytes.Buffer
	if err := a.runner().Run(ctx, executil.Command{Path: rg, Args: []string{"--version"}, Stdout: &out}); err != nil {
		return "version unknown"
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	if first = strings.TrimSpace(first); first != "" {
		return first
	}
	return "version unknown"
}

func otherVersions(installed []konan.Distribution, current konan.Distribution) string {
	var versions []string
	for _, d := range installed {
		if d.Home != current.Home {
			versions = append(versions, d.Version)
		}
	}
	return strings.Join(versions, ", ")
}

func humanSize(n int64) string {
	const unit = 1024
	switch {
	case n >= unit*unit*unit:
		return fmt.Sprintf("%.1f GB", float64(n)/(unit*unit*unit))
	case n >= unit*unit:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	case n >= unit:
		return fmt.Sprintf("%.1f kB", float64(n)/unit)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
