package adapter

import (
	"bufio"
	"context"
	"fmt"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/provider"
	"github.com/satanyakiv/ksrc-native/internal/xcode"
)

// DepsRequest is the input of Deps.
type DepsRequest struct {
	Common
}

// Deps lists what can be searched, one module per line:
//
//	kn.platform:<Framework>:<kotlinVersion> (<target>, dump cached: yes|no)
//	kn.stdlib:native:<kotlinVersion> (extracted: yes|no, <zip>)
//	sdk.apple:<Framework>:<sdk> (<n> frameworks with headers, <sdk path>)
//
// The platform lines cover the libraries of --target. An sdk.apple line
// stands for all frameworks of one installed SDK; <Framework> is literal
// there. A source that is not available is reported on stderr and skipped;
// Deps fails when nothing can be listed, when the caller named a
// distribution (--konan-home, --kotlin-version) that cannot be used, or when
// --target is not a target of the distribution that was found.
func (a *Adapter) Deps(ctx context.Context, req DepsRequest) error {
	return coded(a.deps(ctx, req))
}

func (a *Adapter) deps(ctx context.Context, req DepsRequest) error {
	r, err := a.newResolver(req.Common)
	if err != nil {
		return err
	}
	// A distribution that was asked for by name is not optional: skipping it
	// would hide a typo behind a list of SDKs and a zero exit code.
	if req.KonanHome != "" || req.KotlinVersion != "" {
		if _, err := r.distribution(); err != nil {
			return err
		}
	}
	// The same holds for the target: with a distribution to check it against,
	// a wrong one is a wrong argument, not a missing source.
	if dist, err := r.distribution(); err == nil {
		if err := dist.RequireTarget(r.common.target()); err != nil {
			return err
		}
	}
	out := bufio.NewWriter(a.Stdout)
	printed := 0
	var firstErr error
	skip := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
		fmt.Fprintf(a.Stderr, "ksrc-native: skipped: %v\n", err)
	}

	if err := a.depsKonan(r, out, &printed); err != nil {
		skip(err)
	}
	if err := a.depsApple(ctx, out, &printed); err != nil {
		skip(err)
	}

	if err := out.Flush(); err != nil {
		return err
	}
	if printed == 0 {
		if firstErr != nil {
			return firstErr
		}
		return errs.New(errs.NoSources, "nothing to list", "run `ksrc-native doctor`")
	}
	return nil
}

// depsKonan lists the platform libraries of the target and the stdlib.
func (a *Adapter) depsKonan(r *resolver, out *bufio.Writer, printed *int) error {
	target := r.common.target()
	platform, err := r.platform(target)
	if err != nil {
		return err
	}
	for _, name := range platform.Dist.Frameworks(target) {
		coord := provider.Coordinate{Group: provider.PlatformGroup, Name: name, Version: platform.Dist.Version}
		fmt.Fprintf(out, "%s (%s, dump cached: %s)\n", coord, target, yesNo(platform.IsCached(target, name)))
		*printed++
	}

	stdlib, err := r.stdlib()
	if err != nil {
		return err
	}
	if stdlib.Available() {
		coord := provider.Coordinate{Group: provider.StdlibGroup, Name: provider.StdlibName, Version: stdlib.Dist.Version}
		fmt.Fprintf(out, "%s (extracted: %s, %s)\n", coord, yesNo(stdlib.Cached()), stdlib.Dist.StdlibSources())
		*printed++
	}
	return nil
}

// depsApple lists the installed Apple SDKs.
func (a *Adapter) depsApple(ctx context.Context, out *bufio.Writer, printed *int) error {
	sdks, err := xcode.List(ctx, a.Runner)
	if err != nil {
		return err
	}
	if len(sdks) == 0 {
		return errs.New(errs.NoXcode, "the selected developer directory has no SDKs",
			"install Xcode, then select it with `sudo xcode-select -s /Applications/Xcode.app`")
	}
	for _, sdk := range sdks {
		fmt.Fprintf(out, "%s:<Framework>:%s (%d frameworks with headers, %s)\n", provider.AppleGroup, sdk.Name, len(sdk.Frameworks()), sdk.Path)
		*printed++
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
