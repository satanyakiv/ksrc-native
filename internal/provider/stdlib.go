package provider

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"context"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/konan"
)

const (
	// StdlibGroup is the synthetic group of the Kotlin/Native standard
	// library sources: kn.stdlib:native[:<kotlinVersion>].
	StdlibGroup = "kn.stdlib"
	// StdlibName is the only module of the group.
	StdlibName = "native"
	// StdlibNamespace is the cache namespace of extracted stdlib sources.
	StdlibNamespace = "stdlib-v1"
)

// Stdlib serves the sources of the native standard library, including
// kotlinx.cinterop. They ship inside the distribution as a zip, which is
// extracted into the cache on first use.
type Stdlib struct {
	Dist  konan.Distribution
	Cache cache.Store
	Log   io.Writer // progress messages; may be nil
}

// Group implements Provider.
func (s *Stdlib) Group() string { return StdlibGroup }

// Available reports whether the distribution ships the sources zip.
func (s *Stdlib) Available() bool {
	info, err := os.Stat(s.Dist.StdlibSources())
	return err == nil && info.Mode().IsRegular()
}

// Cached reports whether the sources are already extracted.
func (s *Stdlib) Cached() bool {
	key, err := s.key()
	if err != nil {
		return false
	}
	_, ok := s.Cache.Get(StdlibNamespace, key)
	return ok
}

// Locate implements Provider: the origin is the sources zip.
func (s *Stdlib) Locate(_ context.Context, req Request) (Location, error) {
	coord, err := s.check(req.Coordinate)
	if err != nil {
		return Location{}, err
	}
	return Location{Coordinate: coord, Path: s.Dist.StdlibSources()}, nil
}

// Resolve implements Provider.
func (s *Stdlib) Resolve(_ context.Context, req Request) (Source, error) {
	coord, err := s.check(req.Coordinate)
	if err != nil {
		return Source{}, err
	}
	key, err := s.key()
	if err != nil {
		return Source{}, err
	}
	zipPath := s.Dist.StdlibSources()
	entry, err := s.Cache.Ensure(StdlibNamespace, key, func(srcDir string) (cache.Meta, error) {
		if s.Log != nil {
			fmt.Fprintf(s.Log, "ksrc-native: extracting %s (first use)...\n", coord)
		}
		files, err := unzip(zipPath, srcDir)
		if err != nil {
			return cache.Meta{}, errs.Wrap(err, errs.NoSources,
				fmt.Sprintf("cannot extract %s: %v", zipPath, err),
				"reinstall the distribution: delete "+s.Dist.Home+" and run a Kotlin/Native Gradle task once")
		}
		return cache.Meta{Coordinate: coord.String(), Origin: zipPath, Files: files}, nil
	})
	if err != nil {
		return Source{}, err
	}
	return Source{Coordinate: coord, Root: entry.Src, Origin: zipPath}, nil
}

// check validates the coordinate and the presence of the zip, and returns the
// coordinate with its version filled in.
func (s *Stdlib) check(coord Coordinate) (Coordinate, error) {
	if coord.Group != StdlibGroup {
		return Coordinate{}, errs.New(errs.BadCoord, fmt.Sprintf("the stdlib provider cannot resolve %s", coord), stdlibHint)
	}
	if !strings.EqualFold(coord.Name, StdlibName) {
		return Coordinate{}, errs.New(errs.NoSources, fmt.Sprintf("the group %s has no module %q", StdlibGroup, coord.Name), stdlibHint)
	}
	if coord.Version != "" && coord.Version != s.Dist.Version {
		return Coordinate{}, errs.New(errs.BadCoord,
			fmt.Sprintf("%s asks for Kotlin %s, but the distribution in use is %s", coord, coord.Version, s.Dist.Version),
			"drop the version from the coordinate or pass --kotlin-version "+coord.Version)
	}
	if !s.Available() {
		return Coordinate{}, errs.New(errs.NoSources,
			fmt.Sprintf("the distribution has no stdlib sources at %s", s.Dist.StdlibSources()),
			"reinstall the distribution: delete "+s.Dist.Home+" and run a Kotlin/Native Gradle task once")
	}
	return Coordinate{Group: StdlibGroup, Name: StdlibName, Version: s.Dist.Version}, nil
}

const stdlibHint = "use kn.stdlib:native[:<kotlinVersion>]"

// key identifies the extracted sources by the zip's path, size and
// modification time.
func (s *Stdlib) key() (string, error) {
	zipPath := s.Dist.StdlibSources()
	info, err := os.Stat(zipPath)
	if err != nil {
		return "", errs.New(errs.NoSources,
			fmt.Sprintf("the distribution has no stdlib sources at %s", zipPath),
			"reinstall the distribution: delete "+s.Dist.Home+" and run a Kotlin/Native Gradle task once")
	}
	realPath := zipPath
	if p, err := filepath.EvalSymlinks(zipPath); err == nil {
		realPath = p
	}
	return cache.Key(realPath, strconv.FormatInt(info.Size(), 10), strconv.FormatInt(info.ModTime().UnixNano(), 10)), nil
}

// unzip extracts the regular files of a zip into dir and returns their
// number. Entries that would land outside dir are rejected.
func unzip(zipPath, dir string) (int, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	count := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			continue // no symbolic links or devices from an archive
		}
		name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return count, fmt.Errorf("the archive entry %q points outside the archive", f.Name)
		}
		if err := extract(f, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func extract(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
