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
	Pattern     string
	Modules     []string // coordinates; empty means "cached platform dumps plus the stdlib"
	AllPlatform bool     // build and search every platform library of the target
	Context     int      // lines of context around each match
	RgArgs      []string // extra arguments for rg; untrusted, see search.NormalizeExtraArgs
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
	// Fail on a forbidden rg argument and on a missing rg before any dump is
	// built. search.Run checks the arguments again; it is the one place that
	// cannot be bypassed.
	if _, err := search.NormalizeExtraArgs(req.RgArgs); err != nil {
		return err
	}
	if _, err := search.FindRipgrep(a.runner()); err != nil {
		return err
	}

	coords := make([]provider.Coordinate, 0, len(req.Modules))
	for _, m := range req.Modules {
		c, err := provider.ParseCoordinate(m)
		if err != nil {
			return err
		}
		coords = append(coords, c)
	}
	r, err := a.newResolver(req.Common, coords...)
	if err != nil {
		return err
	}
	sources, err := a.searchSources(ctx, r, req, coords)
	if err != nil {
		return err
	}

	roots := make([]search.Root, len(sources))
	for i, s := range sources {
		roots[i] = search.Root{Dir: s.Root, Prefix: s.FileID("")}
	}

	out := bufio.NewWriter(a.Stdout)
	printer := &linePrinter{out: out, context: req.Context > 0}
	matches, err := search.Run(ctx, a.runner(), search.Options{
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
		fmt.Fprintf(a.Stderr, "ksrc-native: no matches for %q in %s\n", req.Pattern, describeSources(sources, r.target))
	}
	return nil
}

// searchSources decides what a search covers:
//
//   - the named modules, built on first use;
//   - with --all-platform, also every platform library of the target;
//   - without modules, the platform dumps that are already cached plus the
//     stdlib. A search never builds all platform dumps unless asked.
func (a *Adapter) searchSources(ctx context.Context, r *resolver, req SearchRequest, coords []provider.Coordinate) ([]provider.Source, error) {
	var sources []provider.Source
	seen := make(map[string]bool)
	add := func(s provider.Source) {
		if !seen[s.Root] {
			seen[s.Root] = true
			sources = append(sources, s)
		}
	}

	for _, c := range coords {
		request := r.request(c)
		p, err := r.provider(request)
		if err != nil {
			return nil, err
		}
		src, err := p.Resolve(ctx, request)
		if err != nil {
			return nil, err
		}
		add(src)
	}

	if req.AllPlatform {
		platform, err := r.platform(r.target())
		if err != nil {
			return nil, err
		}
		for _, name := range platform.Dist.Frameworks(r.target()) {
			src, err := platform.Resolve(ctx, r.request(provider.Coordinate{Group: provider.PlatformGroup, Name: name}))
			if err != nil {
				// A library without declarations is not a reason to stop.
				if errs.Code(err) == errs.NoSources {
					continue
				}
				return nil, err
			}
			add(src)
		}
	}

	if len(coords) == 0 {
		platform, err := r.platform(r.target())
		if err != nil {
			return nil, err
		}
		if !req.AllPlatform {
			for _, src := range platform.Cached(r.target()) {
				add(src)
			}
		}
		stdlib, err := r.stdlib()
		if err != nil {
			return nil, err
		}
		if stdlib.Available() {
			src, err := stdlib.Resolve(ctx, r.request(provider.Coordinate{Group: provider.StdlibGroup, Name: provider.StdlibName}))
			if err != nil {
				return nil, err
			}
			add(src)
		}
	}

	if len(sources) == 0 {
		dist, _ := r.distribution()
		return nil, errs.New(errs.NoSources,
			fmt.Sprintf("nothing to search: no platform library is cached for Kotlin %s, target %s, the distribution has no stdlib sources, and no --module was given", dist.Version, r.target()),
			"name the framework to search: ksrc-native search '<pattern>' --module kn.platform:UIKit")
	}
	return sources, nil
}

// describeSources names what was searched, for the no-matches note.
// target is called only when a platform library was searched.
func describeSources(sources []provider.Source, target func() string) string {
	const limit = 4
	names := make([]string, 0, len(sources))
	platform := false
	for _, s := range sources {
		names = append(names, s.Coordinate.String())
		platform = platform || s.Coordinate.Group == provider.PlatformGroup
	}
	text := strings.Join(names, ", ")
	if len(names) > limit {
		text = fmt.Sprintf("%s and %d more modules", strings.Join(names[:limit], ", "), len(names)-limit)
	}
	if platform {
		text += " (" + target() + ")"
	}
	return text
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
