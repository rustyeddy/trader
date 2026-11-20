package trader

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	remotestrat "github.com/rustyeddy/trader/strategy"
)

// remoteStrategy is a strategy that runs in another process on this machine. It
// adapts the transport in package strategy to the engine's strategy interface:
// each onBars sends the frame to the worker and waits for its answer, so the
// engine runs exactly as it does for an in-process strategy.
//
// It cannot return an error from onBars, so the first failure is kept (err), the
// strategy goes quiet, and the trader stops the run at once, before using the
// answer. The failer bridge goes away when onBars can return an error.
type remoteStrategy struct {
	id     string
	remote *remotestrat.Client
	failed error
}

var _ strategy = (*remoteStrategy)(nil)
var _ failer = (*remoteStrategy)(nil)

// dialRemote connects to the worker listening at addr (loopback only).
func dialRemote(id, addr string) (*remoteStrategy, error) {
	r, err := remotestrat.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("remote strategy %s: %w", id, err)
	}
	return &remoteStrategy{id: id, remote: r}, nil
}

func (r *remoteStrategy) name() string { return r.id }

func (r *remoteStrategy) init() error { return nil }

func (r *remoteStrategy) err() error { return r.failed }

// close ends the session; the worker sees the connection close and exits.
func (r *remoteStrategy) close() error { return r.remote.Close() }

// onBars hands the frame to the worker, which answers with no orders. After a
// failure it does nothing.
func (r *remoteStrategy) onBars(ctx context.Context, bars []marketdata.Bar) []*order {
	if r.failed != nil {
		return nil
	}
	if err := r.remote.OnBars(ctx, bars); err != nil {
		r.failed = fmt.Errorf("remote strategy %s: %w", r.id, err)
	}
	return nil
}
