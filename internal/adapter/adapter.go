// Package adapter implements the tool's operations on top of the providers.
// The CLI calls it directly, and the MCP server will call the same functions,
// so both speak one output contract.
package adapter

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/provider"
)

// Internal is the code of a failure that has no code of its own.
const Internal = "E_INTERNAL"

// Adapter holds everything an operation needs from the outside world.
type Adapter struct {
	Runner   executil.Runner
	Stdout   io.Writer           // results
	Stderr   io.Writer           // diagnostics and progress
	Getenv   func(string) string // environment lookup
	UserHome string              // the user's home directory
}

// New returns an Adapter wired to the real environment.
func New(stdout, stderr io.Writer) *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		Runner:   executil.OS{},
		Stdout:   stdout,
		Stderr:   stderr,
		Getenv:   os.Getenv,
		UserHome: home,
	}
}

// Common are the options every operation accepts.
type Common struct {
	KonanHome     string // --konan-home
	KotlinVersion string // --kotlin-version
	Project       string // --project
	Target        string // --target
}

func (c Common) target() string {
	if c.Target == "" {
		return konan.DefaultTarget
	}
	return c.Target
}

// distribution discovers the Kotlin/Native distribution. coordVersion is the
// version named by a coordinate or file-id; it takes the place of
// --kotlin-version and must agree with it.
func (a *Adapter) distribution(c Common, coordVersion string) (konan.Distribution, error) {
	version := c.KotlinVersion
	if coordVersion != "" {
		if version != "" && version != coordVersion {
			return konan.Distribution{}, errs.New(errs.BadCoord,
				fmt.Sprintf("--kotlin-version %s disagrees with the coordinate version %s", version, coordVersion),
				"drop one of the two versions")
		}
		version = coordVersion
	}
	return konan.Discover(konan.DiscoverOptions{
		KonanHome:     c.KonanHome,
		KotlinVersion: version,
		ProjectDir:    c.Project,
		Getenv:        a.Getenv,
		UserHome:      a.UserHome,
		Warn:          a.Stderr,
	})
}

// platform builds the platform provider for a distribution.
func (a *Adapter) platform(dist konan.Distribution) (*provider.Platform, error) {
	store, err := cache.Open(a.Getenv)
	if err != nil {
		return nil, err
	}
	return &provider.Platform{Dist: dist, Runner: a.Runner, Cache: store, Log: a.Stderr}, nil
}

// requirePlatformGroup rejects coordinates of groups this version cannot
// serve.
func requirePlatformGroup(c provider.Coordinate) error {
	if c.Group == provider.PlatformGroup {
		return nil
	}
	return errs.New(errs.BadCoord,
		fmt.Sprintf("unknown group %q in %s", c.Group, c),
		"this version serves platform libraries only: kn.platform:<Framework>[:<kotlinVersion>]")
}

// coded makes sure an error leaves the adapter with a stable code.
func coded(err error) error {
	if err == nil || errs.Code(err) != "" {
		return err
	}
	if errors.Is(err, os.ErrPermission) {
		return errs.Wrap(err, Internal, err.Error(), "check the permissions of the path in the message")
	}
	return errs.Wrap(err, Internal, err.Error(), "run `ksrc-native doctor`")
}
