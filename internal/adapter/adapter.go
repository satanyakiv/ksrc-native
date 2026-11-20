// Package adapter implements the tool's operations on top of the providers.
// The CLI and the MCP server call the same functions, so both speak one
// output contract.
package adapter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"golang.org/x/term"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/executil"
	"github.com/satanyakiv/ksrc-native/internal/konan"
	"github.com/satanyakiv/ksrc-native/internal/provider"
	"github.com/satanyakiv/ksrc-native/internal/xcode"
)

// Internal is the code of a failure that has no code of its own.
const Internal = "E_INTERNAL"

// Adapter holds everything an operation needs from the outside world.
type Adapter struct {
	Runner     executil.Runner     // starts programs; every operation uses it through runner()
	Stdout     io.Writer           // results
	Stderr     io.Writer           // diagnostics and progress
	Stdin      io.Reader           // answers to confirmation prompts
	Getenv     func(string) string // environment lookup
	UserHome   string              // the user's home directory
	IsTerminal func() bool         // whether a person can answer a prompt
	GOOS       string              // the system the tool runs on, as runtime.GOOS names it
	GOARCH     string              // its architecture, as runtime.GOARCH names it

	// limit replaces deadlineOf in tests.
	limit func(program string) time.Duration
}

// New returns an Adapter wired to the real environment.
func New(stdout, stderr io.Writer) *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		Runner:   executil.OS{},
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    os.Stdin,
		Getenv:   os.Getenv,
		UserHome: home,
		IsTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
}

// env reads a variable of the environment the adapter was given.
func (a *Adapter) env(key string) string {
	if a.Getenv == nil {
		return ""
	}
	return a.Getenv(key)
}

// WithOutput returns a copy of the adapter that writes to other streams. The
// MCP server uses it to capture the output of one tool call.
func (a *Adapter) WithOutput(stdout, stderr io.Writer) *Adapter {
	clone := *a
	clone.Stdout, clone.Stderr = stdout, stderr
	return &clone
}

// Common are the options every operation accepts.
type Common struct {
	KonanHome     string // --konan-home
	KotlinVersion string // --kotlin-version
	Project       string // --project
	Target        string // --target
	SDK           string // --sdk
}

func (c Common) sdk() string {
	if c.SDK == "" {
		return xcode.DefaultSDK
	}
	return c.SDK
}

const groupsHint = "use kn.platform:<Framework>[:<kotlinVersion>], kn.stdlib:native[:<kotlinVersion>] or sdk.apple:<Framework>[:<sdk>]"

// usesKonan reports whether a group is served from the Kotlin/Native
// distribution, which makes the version of its coordinates a Kotlin version.
func usesKonan(group string) bool {
	return group == provider.PlatformGroup || group == provider.StdlibGroup
}

func knownGroup(c provider.Coordinate) error {
	switch c.Group {
	case provider.PlatformGroup, provider.StdlibGroup, provider.AppleGroup:
		return nil
	}
	return errs.New(errs.BadCoord, fmt.Sprintf("unknown group %q in %s", c.Group, c), groupsHint)
}

// resolver turns coordinates into sources for one operation. It discovers the
// Kotlin/Native distribution at most once, and only when a kn.* module needs
// it, so sdk.apple works on a machine without Kotlin/Native.
type resolver struct {
	a             *Adapter
	common        Common
	kotlinVersion string // --kotlin-version or the version of a kn.* coordinate

	dist     *konan.Distribution
	distErr  error
	store    *cache.Store
	storeErr error
}

// newResolver validates the groups of the coordinates and the SDK names, and
// settles the one Kotlin version the coordinates and --kotlin-version may
// name.
//
// --sdk, --target and the versions in coordinates may come from an MCP input
// that a model filled in, so they are untrusted. An SDK name is checked here,
// before any provider exists, because xcrun would take a path in its place.
// The target needs the distribution to be checked against; see platform.
func (a *Adapter) newResolver(c Common, coords ...provider.Coordinate) (*resolver, error) {
	if c.SDK != "" && !xcode.ValidSDKName(c.SDK) {
		return nil, xcode.BadSDKName(errs.BadArgs, "--sdk", c.SDK)
	}
	version := ""
	for _, coord := range coords {
		if err := knownGroup(coord); err != nil {
			return nil, err
		}
		if coord.Group == provider.AppleGroup && coord.Version != "" && !xcode.ValidSDKName(coord.Version) {
			return nil, xcode.BadSDKName(errs.BadCoord, "the SDK part of "+coord.String(), coord.Version)
		}
		if !usesKonan(coord.Group) || coord.Version == "" {
			continue
		}
		if version != "" && version != coord.Version {
			return nil, errs.New(errs.BadCoord,
				fmt.Sprintf("the modules name two Kotlin versions, %s and %s", version, coord.Version),
				"search one Kotlin version at a time")
		}
		version = coord.Version
	}
	if c.KotlinVersion != "" {
		if version != "" && version != c.KotlinVersion {
			return nil, errs.New(errs.BadCoord,
				fmt.Sprintf("--kotlin-version %s disagrees with the coordinate version %s", c.KotlinVersion, version),
				"drop one of the two versions")
		}
		version = c.KotlinVersion
	}
	return &resolver{a: a, common: c, kotlinVersion: version}, nil
}

func (r *resolver) distribution() (konan.Distribution, error) {
	if r.dist == nil && r.distErr == nil {
		d, err := konan.Discover(konan.DiscoverOptions{
			KonanHome:     r.common.KonanHome,
			KotlinVersion: r.kotlinVersion,
			ProjectDir:    r.common.Project,
			Getenv:        r.a.Getenv,
			UserHome:      r.a.UserHome,
			Warn:          r.a.Stderr,
		})
		if err != nil {
			r.distErr = err
		} else {
			r.dist = &d
		}
	}
	if r.distErr != nil {
		return konan.Distribution{}, r.distErr
	}
	return *r.dist, nil
}

func (r *resolver) cache() (cache.Store, error) {
	if r.store == nil && r.storeErr == nil {
		s, err := cache.Open(r.a.Getenv)
		if err != nil {
			r.storeErr = err
		} else {
			r.store = &s
		}
	}
	if r.storeErr != nil {
		return cache.Store{}, r.storeErr
	}
	return *r.store, nil
}

// platform returns the provider of platform libraries for one target. It is
// the only way to get that provider, and it refuses a target that is not the
// exact name of a target of the distribution: the target becomes a directory
// name inside a cache entry and a segment of every file-id.
func (r *resolver) platform(target string) (*provider.Platform, error) {
	dist, err := r.distribution()
	if err != nil {
		return nil, err
	}
	if err := dist.RequireTarget(target); err != nil {
		return nil, err
	}
	store, err := r.cache()
	if err != nil {
		return nil, err
	}
	return &provider.Platform{Dist: dist, Runner: r.a.runner(), Cache: store, Log: r.a.Stderr}, nil
}

func (r *resolver) stdlib() (*provider.Stdlib, error) {
	dist, err := r.distribution()
	if err != nil {
		return nil, err
	}
	store, err := r.cache()
	if err != nil {
		return nil, err
	}
	return &provider.Stdlib{Dist: dist, Cache: store, Log: r.a.Stderr}, nil
}

func (r *resolver) apple() *provider.AppleSDK {
	return &provider.AppleSDK{Runner: r.a.runner()}
}

// provider returns the provider that owns the group of the request's
// coordinate, after checking the request's target when the group uses one.
func (r *resolver) provider(req provider.Request) (provider.Provider, error) {
	switch req.Coordinate.Group {
	case provider.PlatformGroup:
		return r.platform(req.Target)
	case provider.StdlibGroup:
		return r.stdlib()
	case provider.AppleGroup:
		return r.apple(), nil
	}
	return nil, knownGroup(req.Coordinate)
}

// target returns the Kotlin/Native target of the operation: --target when
// the caller gave one, else what the distribution offers, which is ios_arm64
// or, in a distribution without it, the host target. The answer needs the
// distribution, so only code that works on kn.platform may ask; an
// sdk.apple module must not start a search for one.
func (r *resolver) target() string {
	if r.common.Target != "" {
		return r.common.Target
	}
	dist, err := r.distribution()
	if err != nil {
		return konan.DefaultTarget
	}
	return dist.PickTarget(r.a.GOOS, r.a.GOARCH)
}

func (r *resolver) request(c provider.Coordinate) provider.Request {
	req := provider.Request{Coordinate: c, SDK: r.common.sdk()}
	if c.Group == provider.PlatformGroup {
		req.Target = r.target()
	}
	return req
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
