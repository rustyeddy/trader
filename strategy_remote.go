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
type remoteStrategy struct {
	id     string
	client *remotestrat.Client
}

var _ strategy = (*remoteStrategy)(nil)

// dialRemote connects to the worker listening at addr (loopback only).
func dialRemote(id, addr string) (*remoteStrategy, error) {
	c, err := remotestrat.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("remote strategy %s: %w", id, err)
	}
	return &remoteStrategy{id: id, client: c}, nil
}

func (r *remoteStrategy) name() string { return r.id }

func (r *remoteStrategy) init() error { return nil }

// close ends the session; the worker sees the connection close and exits.
func (r *remoteStrategy) close() error { return r.client.Close() }

// onBars hands the frame to the worker, which answers with no orders. Any failure
// is returned and stops the run.
func (r *remoteStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return nil, r.client.OnBars(ctx, bars)
}
