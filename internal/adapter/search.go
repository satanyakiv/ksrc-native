package adapter

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/provider"
	"github.com/satanyakiv/ksrc-native/internal/search"
)

// SearchRequest is the input of Search.
type SearchRequest struct {
	Common
	Pattern string
	Modules []string // coordinates; empty means "every cached platform dump"
	Context int      // lines of context around each match
	RgArgs  []string // extra arguments for rg
}

// Search runs a regular-expression search and prints one line per match:
//
//	<file-id> <line>:<col>:<text>
//
// Context lines are printed as "<file-id> <line>-<text>", and "--" separates
// groups that are not adjacent.
func (a *Adapter) Search(ctx context.Context, req SearchRequest) error {
	return coded(a.search(ctx, req))
}

func (a *Adapter) search(ctx context.Context, req SearchRequest) error {
	if strings.TrimSpace(req.Pattern) == "" {
		return errs.New(errs.BadArgs, "the search pattern is empty",
			"pass a regular expression, for example: ksrc-native search 'class platform/UIKit/UIApplication' --module kn.platform:UIKit")
	}
	if req.Context < 0 {
		return errs.New(errs.BadArgs, "--context must not be negative", "pass --context 3")
	}
	// Fail on a missing rg before any dump is built.
	if _, err := search.FindRipgrep(a.Runner); err != nil {
		return err
	}

	coords, version, err := parseModules(req.Modules)
	if err != nil {
		return err
	}
	dist, err := a.distribution(req.Common, version)
	if err != nil {
		return err
	}
	platform, err := a.platform(dist)
	if err != nil {
		return err
	}

	var sources []provider.Source
	if len(coords) == 0 {
		sources = platform.Cached(req.target())
		if len(sources) == 0 {
			return errs.New(errs.NoSources,
				fmt.Sprintf("no platform library is cached yet for Kotlin %s, target %s, and no --module was given", dist.Version, req.target()),
				"name the framework to search: ksrc-native search '<pattern>' --module kn.platform:UIKit")
		}
	}
	for _, c := range coords {
		src, err := platform.Resolve(ctx, provider.Request{Coordinate: c, Target: req.target()})
		if err != nil {
			return err
		}
		sources = append(sources, src)
	}

	roots := make([]search.Root, len(sources))
	names := make([]string, len(sources))
	for i, s := range sources {
		roots[i] = search.Root{Dir: s.Root, Prefix: s.FileID("")}
		names[i] = s.Coordinate.String()
	}

	out := bufio.NewWriter(a.Stdout)
	printer := &linePrinter{out: out, context: req.Context > 0}
	matches, err := search.Run(ctx, a.Runner, search.Options{
		Pattern:   req.Pattern,
		Roots:     roots,
		Context:   req.Context,
		ExtraArgs: req.RgArgs,
	}, printer.print)
	if flushErr := out.Flush(); err == nil {
		err = flushErr
	}
	if err != nil {
		return err
	}
	if matches == 0 {
		fmt.Fprintf(a.Stderr, "ksrc-native: no matches for %q in %s (%s)\n", req.Pattern, strings.Join(names, ", "), req.target())
	}
	return nil
}

// parseModules parses the --module values and returns the one Kotlin version
// they name, if any.
func parseModules(modules []string) ([]provider.Coordinate, string, error) {
	var coords []provider.Coordinate
	version := ""
	for _, m := range modules {
		c, err := provider.ParseCoordinate(m)
		if err != nil {
			return nil, "", err
		}
		if err := requirePlatformGroup(c); err != nil {
			return nil, "", err
		}
		if c.Version != "" {
			if version != "" && version != c.Version {
				return nil, "", errs.New(errs.BadCoord,
					fmt.Sprintf("the modules name two Kotlin versions, %s and %s", version, c.Version),
					"search one Kotlin version at a time")
			}
			version = c.Version
		}
		coords = append(coords, c)
	}
	return coords, version, nil
}

// linePrinter writes search lines and the "--" separators between context
// groups.
type linePrinter struct {
	out     *bufio.Writer
	context bool

	printed    bool
	lastFileID string
	lastNumber int
}

func (p *linePrinter) print(l search.Line) error {
	if p.context && p.printed && (l.FileID != p.lastFileID || l.Number > p.lastNumber+1) {
		if _, err := p.out.WriteString("--\n"); err != nil {
			return err
		}
	}
	p.printed, p.lastFileID, p.lastNumber = true, l.FileID, l.Number
	if _, err := p.out.WriteString(search.Format(l)); err != nil {
		return err
	}
	return p.out.WriteByte('\n')
}
