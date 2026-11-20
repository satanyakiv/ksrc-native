package provider

import (
	"context"
	"path/filepath"
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

// Location says where the data of a module comes from, without preparing it
// for search.
type Location struct {
	Coordinate Coordinate // always carries a version
	Path       string     // a klib directory, a zip file or a Headers directory
}

// Provider resolves the coordinates of one group.
type Provider interface {
	// Group returns the synthetic group the provider owns, for example
	// kn.platform.
	Group() string
	// Resolve makes the sources of a module available and returns where they
	// are. It may build and cache them on first use.
	Resolve(ctx context.Context, req Request) (Source, error)
	// Locate returns the origin of a module. It never builds anything.
	Locate(ctx context.Context, req Request) (Location, error)
}
