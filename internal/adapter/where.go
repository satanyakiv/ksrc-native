package adapter

import (
	"context"
	"fmt"

	"github.com/satanyakiv/ksrc-native/internal/provider"
)

// WhereRequest is the input of Where.
type WhereRequest struct {
	Common
	Coordinate string
}

// Where prints the path a module is read from: the klib directory of a
// platform library, the stdlib sources zip or the Headers directory of an
// SDK framework. It builds nothing.
func (a *Adapter) Where(ctx context.Context, req WhereRequest) error {
	return coded(a.where(ctx, req))
}

func (a *Adapter) where(ctx context.Context, req WhereRequest) error {
	coord, err := provider.ParseCoordinate(req.Coordinate)
	if err != nil {
		return err
	}
	r, err := a.newResolver(req.Common, coord)
	if err != nil {
		return err
	}
	request := r.request(coord)
	p, err := r.provider(request)
	if err != nil {
		return err
	}
	loc, err := p.Locate(ctx, request)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Stdout, loc.Path)
	return err
}
