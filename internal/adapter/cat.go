package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/cat"
	"github.com/satanyakiv/ksrc-native/internal/errs"
	"github.com/satanyakiv/ksrc-native/internal/provider"
)

// CatRequest is the input of Cat.
type CatRequest struct {
	Common
	FileID  string // as printed by search
	Lines   string // "A,B"; empty prints the whole file
	Numbers bool   // prefix lines with their numbers
}

// Cat prints the file a file-id names, or a range of its lines.
func (a *Adapter) Cat(ctx context.Context, req CatRequest) error {
	return coded(a.cat(ctx, req))
}

func (a *Adapter) cat(ctx context.Context, req CatRequest) error {
	id, err := provider.ParseFileID(req.FileID)
	if err != nil {
		return err
	}
	lines, err := cat.ParseLines(req.Lines)
	if err != nil {
		return err
	}
	r, err := a.newResolver(req.Common, id.Coordinate)
	if err != nil {
		return err
	}

	request := r.request(id.Coordinate)
	if id.Coordinate.Group == provider.PlatformGroup {
		// The first segment of a platform file-id is the target, so the
		// file-id alone decides it and --target is not consulted.
		target, _, ok := strings.Cut(id.Inner, "/")
		if !ok {
			return errs.New(errs.BadFileID,
				fmt.Sprintf("the file-id %q has no target segment", req.FileID),
				"copy a file-id from `ksrc-native search` output, for example kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt")
		}
		request.Target = target
	}

	p, err := r.provider(id.Coordinate)
	if err != nil {
		return err
	}
	src, err := p.Resolve(ctx, request)
	if err != nil {
		return err
	}

	path := filepath.Join(src.Root, filepath.FromSlash(id.Inner))
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return errs.New(errs.NoFile,
			fmt.Sprintf("%s has no file %s", src.Coordinate, id.Inner),
			fmt.Sprintf("find the file with: ksrc-native search '<pattern>' --module %s", src.Coordinate))
	}
	return cat.Print(a.Stdout, path, lines, req.Numbers)
}
