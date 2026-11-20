package provider

import (
	"context"
	"path/filepath"
)

// Request asks a provider for the sources of one module.
type Request struct {
	Coordinate Coordinate
	Target     string // Kotlin/Native target, for example ios_arm64
}

// Source is a module resolved to files on disk.
type Source struct {
	Coordinate Coordinate // always carries a version
	Root       string     // directory to search; inner paths of file-ids are relative to it
	Origin     string     // where the data comes from, for example the klib directory
}

// FileID returns the file-id of a path below Root, given relative to it.
func (s Source) FileID(rel string) string {
	return FileID{Coordinate: s.Coordinate, Inner: filepath.ToSlash(rel)}.String()
}

// Provider resolves the coordinates of one group.
type Provider interface {
	// Group returns the synthetic group the provider owns, for example
	// kn.platform.
	Group() string
	// Resolve makes the sources of a module available and returns where they
	// are. It may build and cache them on first use.
	Resolve(ctx context.Context, req Request) (Source, error)
}
