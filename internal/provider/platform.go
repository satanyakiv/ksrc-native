package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/dump"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
)

// PlatformGroup is the synthetic group of Kotlin/Native platform libraries:
// kn.platform:<Framework>[:<kotlinVersion>].
const PlatformGroup = "kn.platform"

// PlatformNamespace is the cache namespace of split platform dumps. The
// suffix changes when the layout of an entry changes.
const PlatformNamespace = "platform-dumps-v1"

// Platform serves the Kotlin signatures of platform klibs (platform.UIKit,
// platform.Foundation, ...). A klib has no sources, so the provider runs
// `klib dump-metadata` once per library and target, splits the dump into one
// file per class and caches the result.
type Platform struct {
	Dist   konan.Distribution
	Runner executil.Runner
	Cache  cache.Store
	Log    io.Writer // progress messages; may be nil
}

// Group implements Provider.
func (p *Platform) Group() string { return PlatformGroup }

// Resolve implements Provider. The files of the returned Source live under
// "<Root>/<target>/", so the target is part of every file-id.
func (p *Platform) Resolve(ctx context.Context, req Request) (Source, error) {
	coord := req.Coordinate
	if coord.Group != PlatformGroup {
		return Source{}, errs.New(errs.BadCoord,
			fmt.Sprintf("the platform provider cannot resolve %s", coord), coordinateHint)
	}
	if coord.Version != "" && coord.Version != p.Dist.Version {
		return Source{}, errs.New(errs.BadCoord,
			fmt.Sprintf("%s asks for Kotlin %s, but the distribution in use is %s", coord, coord.Version, p.Dist.Version),
			"drop the version from the coordinate or pass --kotlin-version "+coord.Version)
	}
	target := req.Target
	if target == "" {
		target = konan.DefaultTarget
	}

	klib, err := p.Dist.FindPlatformKlib(target, coord.Name)
	if err != nil {
		return Source{}, err
	}
	resolved := Coordinate{Group: PlatformGroup, Name: klib.Framework, Version: p.Dist.Version}

	entry, err := p.Cache.Ensure(PlatformNamespace, platformKey(klib.Path), func(srcDir string) (cache.Meta, error) {
		return p.build(ctx, resolved, klib, srcDir)
	})
	if err != nil {
		return Source{}, err
	}
	return Source{Coordinate: resolved, Root: entry.Src, Origin: klib.Path}, nil
}

// build dumps one klib and writes the split files into <srcDir>/<target>/.
func (p *Platform) build(ctx context.Context, coord Coordinate, klib konan.PlatformKlib, srcDir string) (cache.Meta, error) {
	p.logf("ksrc-native: dumping %s for %s (first use, a few seconds)...", coord, klib.Target)

	var out bytes.Buffer
	if err := p.Dist.DumpMetadata(ctx, p.Runner, klib.Path, &out); err != nil {
		return cache.Meta{}, err
	}
	result, err := dump.Split(&out)
	if err != nil {
		return cache.Meta{}, err
	}
	if len(result.Files) == 0 {
		return cache.Meta{}, errs.New(errs.NoSources,
			fmt.Sprintf("%s has no declarations for target %s", coord, klib.Target),
			"search another framework")
	}
	if err := result.WriteTo(filepath.Join(srcDir, klib.Target)); err != nil {
		return cache.Meta{}, errs.Wrap(err, errs.Cache, "cannot write the split dump: "+err.Error(), "set "+cache.EnvDir+" to a writable directory")
	}
	return cache.Meta{
		Coordinate: coord.String(),
		Target:     klib.Target,
		Origin:     klib.Path,
		Files:      len(result.Files),
	}, nil
}

// Cached returns the platform libraries of this distribution and target whose
// dumps are already in the cache. It never builds anything.
func (p *Platform) Cached(target string) []Source {
	if target == "" {
		target = konan.DefaultTarget
	}
	var sources []Source
	for _, e := range p.Cache.List(PlatformNamespace) {
		coord, err := ParseCoordinate(e.Meta.Coordinate)
		if err != nil || coord.Group != PlatformGroup || coord.Version != p.Dist.Version || e.Meta.Target != target {
			continue
		}
		// An entry of another distribution with the same version (for example a
		// second KONAN_DATA_DIR) is not ours.
		if !strings.HasPrefix(e.Meta.Origin, p.Dist.Home+string(filepath.Separator)) {
			continue
		}
		sources = append(sources, Source{Coordinate: coord, Root: e.Src, Origin: e.Meta.Origin})
	}
	return sources
}

// platformKey identifies a dump by the klib's real path and the compiler
// version in its manifest, so a distribution replaced in place gets a new
// entry.
func platformKey(klibDir string) string {
	realPath := klibDir
	if p, err := filepath.EvalSymlinks(klibDir); err == nil {
		realPath = p
	}
	compiler := ""
	if manifest, err := konan.Manifest(klibDir); err == nil {
		compiler = manifest["compiler_version"]
	}
	return cache.Key(realPath, compiler)
}

func (p *Platform) logf(format string, args ...any) {
	if p.Log != nil {
		fmt.Fprintf(p.Log, format+"\n", args...)
	}
}
