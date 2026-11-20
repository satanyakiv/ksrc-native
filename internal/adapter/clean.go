package adapter

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/cache"
	"github.com/satanyakiv/ksrc-native/internal/provider"
)

// CleanRequest is the input of Clean.
type CleanRequest struct {
	Yes    bool // delete without asking
	DryRun bool // never delete
}

// cacheNamespaces are the directories Clean owns. Nothing else below the
// cache root is ever deleted.
var cacheNamespaces = []string{provider.PlatformNamespace, provider.StdlibNamespace}

// Clean shows what the cache holds and deletes it. It deletes only with Yes,
// or after a "y" answer when a person is at the terminal. Without a terminal
// and without Yes it is a dry run, so a script or an agent cannot delete the
// cache by accident.
func (a *Adapter) Clean(_ context.Context, req CleanRequest) error {
	return coded(a.clean(req))
}

func (a *Adapter) clean(req CleanRequest) error {
	store, err := cache.Open(a.Getenv)
	if err != nil {
		return err
	}

	var total int64
	entries := 0
	for _, usage := range store.Usage(cacheNamespaces...) {
		total += usage.Size
		for _, e := range usage.Entries {
			entries++
			line := e.Meta.Coordinate
			if e.Meta.Target != "" {
				line += " (" + e.Meta.Target + ")"
			}
			fmt.Fprintf(a.Stdout, "%s %s\n", line, humanSize(e.Size))
		}
	}
	if total == 0 && entries == 0 {
		fmt.Fprintf(a.Stdout, "the cache is empty: %s\n", store.Root)
		return nil
	}
	fmt.Fprintf(a.Stdout, "total: %d entries, %s in %s\n", entries, humanSize(total), store.Root)

	switch {
	case req.DryRun:
		fmt.Fprintln(a.Stdout, "dry run: nothing deleted")
		return nil
	case req.Yes:
		// Proceed.
	case a.IsTerminal != nil && a.IsTerminal():
		fmt.Fprintf(a.Stdout, "Delete %d entries (%s)? [y/N] ", entries, humanSize(total))
		if !confirmed(a) {
			fmt.Fprintln(a.Stdout, "nothing deleted")
			return nil
		}
	default:
		fmt.Fprintln(a.Stdout, "dry run (not a terminal): nothing deleted; pass -y to delete")
		return nil
	}

	if err := store.Remove(cacheNamespaces...); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "deleted %d entries, %s freed\n", entries, humanSize(total))
	return nil
}

// confirmed reads one line from stdin and reports whether it says yes.
func confirmed(a *Adapter) bool {
	if a.Stdin == nil {
		return false
	}
	answer, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}
