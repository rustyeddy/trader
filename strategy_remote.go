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

	// refs is the worker's label for each order it requested, to echo on the
	// order's events. pending is what the trader has told it since the last
	// group, sent with the next one.
	refs    map[*order]string
	pending []remotestrat.Event
}

var _ strategy = (*remoteStrategy)(nil)
var _ feedbacker = (*remoteStrategy)(nil)
var _ warmer = (*remoteStrategy)(nil)

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

// feedback keeps what the trader did since the last group; the next onBars sends
// it to the worker.
func (r *remoteStrategy) feedback(events []feedbackEvent) {
	for _, e := range events {
		r.pending = append(r.pending, toWireEvent(e, r.refs[e.order]))
		if e.state != stateSubmitted {
			delete(r.refs, e.order) // filled, rejected or cancelled: nothing more to say
		}
	}
}

// warm sends the bars from before the range to the worker as a warm-up: it
// prefills its indicators and answers with no orders. Nothing here reaches the
// broker or the journal, and the worker answering with an order is an error.
func (r *remoteStrategy) warm(ctx context.Context, bars []marketdata.Bar) error {
	return r.client.Warm(ctx, bars)
}

// onBars hands the frame, and the events since the last one, to the worker and
// maps the orders it requests onto the trader's own. Any failure, including an
// order that does not map or validate, is returned and stops the run, and then
// none of the group's orders is processed.
func (r *remoteStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	events := r.pending
	r.pending = nil
	requests, err := r.client.OnBars(ctx, bars, events)
	if err != nil {
		return nil, err
	}
	orders, err := toOrders(requests, bars)
	if err != nil {
		return nil, err
	}
	for i, o := range orders {
		if ref := requests[i].Ref; ref != "" {
			if r.refs == nil {
				r.refs = make(map[*order]string)
			}
			r.refs[o] = ref
		}
	}
	return orders, nil
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
// of that instrument or, for an instrument with no bar in the group, when the
// group became available (its bars' latest AvailableAt, which is the same for
// every bar of a frame). Which instruments a worker may order is not checked here.
func orderTime(instrument string, bars []marketdata.Bar) time.Time {
	var available time.Time
	for _, b := range bars {
		if b.Instrument == instrument {
			return b.Time
		}
		if b.AvailableAt.After(available) {
			available = b.AvailableAt
		}
	}
	return available
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
