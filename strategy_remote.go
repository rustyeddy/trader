package trader

import (
	"context"
	"fmt"
	"time"

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

// onBars hands the frame to the worker and maps the orders it requests onto the
// trader's own. Any failure, including an order that does not map or validate, is
// returned and stops the run, and then none of the group's orders is processed.
func (r *remoteStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	requests, err := r.client.OnBars(ctx, bars)
	if err != nil {
		return nil, err
	}
	return toOrders(requests, bars)
}

// toOrders maps every request, in order, or fails without returning any.
func toOrders(requests []remotestrat.Order, bars []marketdata.Bar) ([]*order, error) {
	var orders []*order
	for i, q := range requests {
		o, err := toOrder(q, orderTime(q.Instrument, bars))
		if err != nil {
			return nil, fmt.Errorf("requested order %d (%s %s): %w", i+1, q.Action, q.Instrument, err)
		}
		orders = append(orders, o)
	}
	return orders, nil
}

// orderTime is the time an order is made on: the opening time of the group's bar
// of that instrument or, for an instrument with no bar in the group, of the
// group's first bar. Which instruments a worker may order is not checked here.
func orderTime(instrument string, bars []marketdata.Bar) time.Time {
	for _, b := range bars {
		if b.Instrument == instrument {
			return b.Time
		}
	}
	if len(bars) > 0 {
		return bars[0].Time
	}
	return time.Time{}
}

// toOrder maps one request onto an order and validates it. It sets only what the
// worker may say: the trader stamps the account and decision time, and the
// broker the id, size and state.
func toOrder(q remotestrat.Order, at time.Time) (*order, error) {
	act, err := parseAction(q.Action)
	if err != nil {
		return nil, err
	}
	dir, err := parseDirection(q.Direction)
	if err != nil {
		return nil, err
	}
	typ, err := parseOrderType(q.Type)
	if err != nil {
		return nil, err
	}
	o := &order{
		instrument: q.Instrument,
		action:     act,
		direction:  dir,
		orderType:  typ,
		price:      q.Price,
		stopLoss:   q.StopLoss,
		takeProfit: q.TakeProfit,
		Time:       at,
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	return o, nil
}

// parseAction accepts the actions a worker may request. amend is not one of them
// yet.
func parseAction(s string) (action, error) {
	switch s {
	case "open":
		return open, nil
	case "add":
		return add, nil
	case "close":
		return close, nil
	case "reduce":
		return reduce, nil
	}
	return noAction, fmt.Errorf("unsupported action %q", s)
}

func parseDirection(s string) (direction, error) {
	switch s {
	case "":
		return noDirection, nil
	case "long":
		return long, nil
	case "short":
		return short, nil
	}
	return noDirection, fmt.Errorf("unknown direction %q", s)
}

func parseOrderType(s string) (orderType, error) {
	switch s {
	case "", "market":
		return marketOrder, nil
	case "limit":
		return limitOrder, nil
	case "stop":
		return stopOrder, nil
	}
	return noOrder, fmt.Errorf("unknown order type %q", s)
}
