package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Request asks a provider for the sources of one module.
type Request struct {
	Coordinate Coordinate
	Target     string // Kotlin/Native target, for example ios_arm64; used by kn.platform
	SDK        string // Apple SDK name, for example iphoneos; used by sdk.apple
}

// Source is a module resolved to files on disk.
type Source struct {
	Coordinate Coordinate // always carries a version
	Root       string     // directory to search; inner paths of file-ids are relative to it
	Origin     string     // where the data comes from: a klib directory, a zip file or a Headers directory
}

// FileID returns the file-id of a path below Root, given relative to it.
func (s Source) FileID(rel string) string {
	return FileID{Coordinate: s.Coordinate, Inner: filepath.ToSlash(rel)}.String()
}

// File returns the path of the regular file that an inner path names, and
// false when there is none below Root.
//
// ParseFileID already keeps an inner path below the root. This is the second
// check, at the place where a file is opened, so that one mistake in the
// parser does not let a file-id read any file of the user. It compares real
// paths: a symbolic link inside the root that points outside is refused too.
// ripgrep does not follow such a link either, so no search prints a file-id
// that this refuses.
func (s Source) File(inner string) (string, bool) {
	root, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", false
	}
	path, err := filepath.EvalSymlinks(filepath.Join(s.Root, filepath.FromSlash(inner)))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// Location says where the data of a module comes from, without preparing it
// for search.
type Location struct {
	Coordinate Coordinate // always carries a version
	Path       string     // a klib directory, a zip file or a Headers directory
}

// Provider resolves the coordinates of one group.
type Provider interface {
	// Resolve makes the sources of a module available and returns where they
	// are. It may build and cache them on first use.
	Resolve(ctx context.Context, req Request) (Source, error)
	// Locate returns the origin of a module. It never builds anything.
	Locate(ctx context.Context, req Request) (Location, error)
}
