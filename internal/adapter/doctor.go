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
)

// DoctorFailed is the code Doctor returns when a check fails.
const DoctorFailed = "E_DOCTOR"

// DoctorRequest is the input of Doctor.
type DoctorRequest struct {
	Common
}

// Doctor checks the environment and prints one line per check:
//
//	<name>: ok - <detail>
//	<name>: MISSING - <what to do>
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

	// ripgrep
	if rg, err := search.FindRipgrep(a.Runner); err != nil {
		report("rg", false, err.Error())
	} else {
		report("rg", true, fmt.Sprintf("%s (%s)", rg, a.rgVersion(ctx, rg)))
	}

	// Kotlin/Native distribution, klib tool and platform libraries
	dist, err := a.distribution(req.Common, "")
	if err != nil {
		report("kotlin-native", false, err.Error())
		report("klib", false, "needs a Kotlin/Native distribution")
		report("platform", false, "needs a Kotlin/Native distribution")
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

		target := req.target()
		if frameworks := dist.Frameworks(target); len(frameworks) == 0 {
			report("platform", false, fmt.Sprintf("no platform libraries for target %s; available targets: %s", target, strings.Join(dist.Targets(), ", ")))
		} else {
			report("platform", true, fmt.Sprintf("%d libraries for %s (%d targets in the distribution)", len(frameworks), target, len(dist.Targets())))
		}
	}

	// cache
	if store, err := cache.Open(a.Getenv); err != nil {
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
	if err := a.Runner.Run(ctx, executil.Command{Path: rg, Args: []string{"--version"}, Stdout: &out}); err != nil {
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
