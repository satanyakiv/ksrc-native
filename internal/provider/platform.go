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

// Locate implements Provider: the origin is the klib directory.
func (p *Platform) Locate(_ context.Context, req Request) (Location, error) {
	coord, klib, err := p.find(req)
	if err != nil {
		return Location{}, err
	}
	return Location{Coordinate: coord, Path: klib.Path}, nil
}

// find validates the request and locates the klib. The returned coordinate
// has the canonical framework name and the version filled in.
func (p *Platform) find(req Request) (Coordinate, konan.PlatformKlib, error) {
	coord := req.Coordinate
	if coord.Group != PlatformGroup {
		return Coordinate{}, konan.PlatformKlib{}, errs.New(errs.BadCoord,
			fmt.Sprintf("the platform provider cannot resolve %s", coord), coordinateHint)
	}
	if coord.Version != "" && coord.Version != p.Dist.Version {
		return Coordinate{}, konan.PlatformKlib{}, errs.New(errs.BadCoord,
			fmt.Sprintf("%s asks for Kotlin %s, but the distribution in use is %s", coord, coord.Version, p.Dist.Version),
			"drop the version from the coordinate or pass --kotlin-version "+coord.Version)
	}
	target := req.Target
	if target == "" {
		target = konan.DefaultTarget
	}
	klib, err := p.Dist.FindPlatformKlib(target, coord.Name)
	if err != nil {
		return Coordinate{}, konan.PlatformKlib{}, err
	}
	return Coordinate{Group: PlatformGroup, Name: klib.Framework, Version: p.Dist.Version}, klib, nil
}

// Resolve implements Provider. The files of the returned Source live under
// "<Root>/<target>/", so the target is part of every file-id.
func (p *Platform) Resolve(ctx context.Context, req Request) (Source, error) {
	resolved, klib, err := p.find(req)
	if err != nil {
		return Source{}, err
	}

	entry, err := p.Cache.Ensure(PlatformNamespace, platformKey(klib.Path), func(srcDir string) (cache.Meta, error) {
		return p.build(ctx, resolved, klib, srcDir)
	})
	if err != nil {
		return Source{}, err
	}
	p.dropOtherFormats(resolved, klib, entry)
	return Source{Coordinate: resolved, Root: entry.Src, Origin: klib.Path}, nil
}

// dropOtherFormats removes the entries of the same klib and target that
// another version of the splitter wrote. The format is part of the key, so
// nothing reads them again; left on disk they are a second line for the same
// library in clean and in the count of doctor. It runs on every use, not
// only after a build: an older binary that is still in use can write such an
// entry at any time. A removal that fails is not a failure of the search.
func (p *Platform) dropOtherFormats(coord Coordinate, klib konan.PlatformKlib, current cache.Entry) {
	removed := 0
	for _, e := range p.Cache.List(PlatformNamespace) {
		if e.Dir == current.Dir || e.Meta.Format == dump.FormatVersion || e.Meta.Origin != klib.Path || e.Meta.Target != klib.Target {
			continue
		}
		if p.Cache.Drop(e) == nil {
			removed++
		}
	}
	switch {
	case removed == 1:
		p.logf("ksrc-native: removed 1 cache entry of another dump format for %s (%s)", coord, klib.Target)
	case removed > 1:
		p.logf("ksrc-native: removed %d cache entries of another dump format for %s (%s)", removed, coord, klib.Target)
	}
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
		Format:     dump.FormatVersion,
	}, nil
}

// IsCached reports whether the dump of a framework is already in the cache.
func (p *Platform) IsCached(target, framework string) bool {
	if target == "" {
		target = konan.DefaultTarget
	}
	klib, err := p.Dist.FindPlatformKlib(target, framework)
	if err != nil {
		return false
	}
	_, ok := p.Cache.Get(PlatformNamespace, platformKey(klib.Path))
	return ok
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
		// An entry of another splitter version has another key, so a named
		// search never reads it. It stays on disk until its library is used
		// again (dropOtherFormats) or until clean; reading it here would
		// print every hit twice next to the rebuilt entry.
		if e.Meta.Format != dump.FormatVersion {
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

// platformKey identifies a dump by the klib's real path, the compiler
// version in its manifest and the version of the splitter. A distribution
// replaced in place gets a new entry, and so does every library after a
// change of what the splitter writes.
func platformKey(klibDir string) string { return platformKeyFor(klibDir, dump.FormatVersion) }

func platformKeyFor(klibDir, format string) string {
	realPath := klibDir
	if p, err := filepath.EvalSymlinks(klibDir); err == nil {
		realPath = p
	}
	compiler := ""
	if manifest, err := konan.Manifest(klibDir); err == nil {
		compiler = manifest["compiler_version"]
	}
	return cache.Key(realPath, compiler, format)
}

func (p *Platform) logf(format string, args ...any) {
	if p.Log != nil {
		fmt.Fprintf(p.Log, format+"\n", args...)
	}
}
