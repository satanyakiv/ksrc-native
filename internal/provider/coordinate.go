// Package provider turns a module coordinate into a directory of searchable
// files. Each kind of source has its own synthetic group; this version ships
// the platform provider only.
package provider

import (
	"fmt"
	"path"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// Coordinate names a module: "group:name" or "group:name:version".
type Coordinate struct {
	Group   string // for example kn.platform
	Name    string // for example UIKit
	Version string // for example 2.4.21; empty means "the discovered version"
}

// String renders the coordinate; the version is left out when empty.
func (c Coordinate) String() string {
	if c.Version == "" {
		return c.Group + ":" + c.Name
	}
	return c.Group + ":" + c.Name + ":" + c.Version
}

const coordinateHint = "use kn.platform:<Framework>[:<kotlinVersion>], for example kn.platform:UIKit"

// ParseCoordinate parses "group:name[:version]".
func ParseCoordinate(s string) (Coordinate, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Coordinate{}, errs.New(errs.BadCoord, fmt.Sprintf("cannot parse the coordinate %q", s), coordinateHint)
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, "!/\\ \t") {
			return Coordinate{}, errs.New(errs.BadCoord, fmt.Sprintf("cannot parse the coordinate %q", s), coordinateHint)
		}
	}
	c := Coordinate{Group: parts[0], Name: parts[1]}
	if len(parts) == 3 {
		c.Version = parts[2]
	}
	return c, nil
}

// fileIDSeparator splits a file-id into the coordinate and the inner path.
const fileIDSeparator = "!/"

// FileID names one file of a module: "group:name:version!/inner/path".
type FileID struct {
	Coordinate Coordinate
	Inner      string // slash-separated path below the module root, cleaned
}

// String renders the file-id.
func (f FileID) String() string {
	return f.Coordinate.String() + fileIDSeparator + f.Inner
}

const fileIDHint = "copy a file-id from `ksrc-native search` output, for example kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt"

// ParseFileID parses a file-id as printed by search. The inner path must stay
// below the module root.
func ParseFileID(s string) (FileID, error) {
	coord, inner, ok := strings.Cut(strings.TrimSpace(s), fileIDSeparator)
	if !ok || inner == "" {
		return FileID{}, errs.New(errs.BadFileID, fmt.Sprintf("cannot parse the file-id %q", s), fileIDHint)
	}
	c, err := ParseCoordinate(coord)
	if err != nil {
		return FileID{}, errs.New(errs.BadFileID, fmt.Sprintf("cannot parse the file-id %q", s), fileIDHint)
	}
	clean := path.Clean("/" + inner)[1:]
	if clean == "" || strings.ContainsRune(inner, '\\') {
		return FileID{}, errs.New(errs.BadFileID, fmt.Sprintf("the file-id %q has no usable inner path", s), fileIDHint)
	}
	return FileID{Coordinate: c, Inner: clean}, nil
}
