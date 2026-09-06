package alpaca

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Events implements broker.Account. See the package doc comment and
// ADR-051 for why this is REST polling rather than a websocket stream.
// The zero EventCursor starts from the beginning of whatever this
// Broker's correlator has recorded since process start (there is no
// durable, cross-restart backlog — see correlator's own doc comment); a
// non-zero cursor resumes strictly after the Sequence it names.
func (h *accountHandle) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	if h.broker.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	return &eventReader{account: h, after: decodeCursor(cursor), done: make(chan struct{})}, nil
}

// decodeCursor decodes an EventCursor produced by encodeCursor as the
// last-delivered Sequence. An empty or malformed cursor decodes to 0,
// meaning "replay from the beginning" — EventCursor's zero value is
// always legal, never an error (ADR-024), matching
// adapters/broker/sim's identical convention.
func decodeCursor(cursor brokerpkg.EventCursor) uint64 {
	if cursor == "" {
		return 0
	}
	v, err := strconv.ParseUint(string(cursor), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func encodeCursor(sequence uint64) brokerpkg.EventCursor {
	return brokerpkg.EventCursor(strconv.FormatUint(sequence, 10))
}

// eventReader is broker.EventReader's polling implementation. It
// delivers whatever the correlator already has buffered above `after`;
// once caught up, it polls Alpaca's order list, synthesizes any new
// events into the shared correlator (visible to every other reader on
// the same account too, matching adapters/broker/sim's shared-log
// convention), and retries — blocking on either the correlator's
// changed signal, ctx, this reader's own Close, or a poll-interval
// timer, never busy-polling.
//
// # Close concurrency
//
// closed and done together give Close the same "wake a blocked Next"
// contract EventReader's own doc comment requires, and give Next a
// safe, race-free way to observe a concurrent Close: closedMu guards
// closed and the one-time close(done); Next reads both under closedMu
// before checking anything else, and additionally selects on done
// inside its blocking wait so a Next already parked in that select
// wakes immediately rather than only on its next loop iteration
// (Copilot/PR #313 review — a plain unsynchronized bool read/write
// here was a real data race, and merely protecting the bool would
// still have left a blocked Next waiting out the rest of the poll
// interval before ever re-checking it).
type eventReader struct {
	account *accountHandle
	after   uint64
	buffer  []brokerpkg.Event

	closedMu sync.Mutex
	closed   bool
	done     chan struct{}
}

var _ brokerpkg.EventReader = (*eventReader)(nil)

// Next implements broker.EventReader. It returns broker.ErrClosed, not
// a package-specific error, once this reader has been closed — the
// same sentinel adapters/broker/sim's own closed-broker paths use
// throughout this package (PR #313 review).
func (r *eventReader) Next(ctx context.Context) (brokerpkg.Event, error) {
	if r.isClosed() {
		return brokerpkg.Event{}, brokerpkg.ErrClosed
	}
	for {
		if len(r.buffer) > 0 {
			ev := r.buffer[0]
			r.buffer = r.buffer[1:]
			r.after = ev.Sequence
			return ev, nil
		}
		if err := ctx.Err(); err != nil {
			return brokerpkg.Event{}, err
		}
		if r.isClosed() {
			return brokerpkg.Event{}, brokerpkg.ErrClosed
		}

		buffered, changed, closed := r.account.broker.corr.eventsAfter(r.after)
		if len(buffered) > 0 {
			r.buffer = buffered
			continue
		}
		if closed {
			return brokerpkg.Event{}, io.EOF
		}

		if err := r.poll(ctx); err != nil {
			return brokerpkg.Event{}, err
		}

		buffered, _, closed = r.account.broker.corr.eventsAfter(r.after)
		if len(buffered) > 0 {
			r.buffer = buffered
			continue
		}
		if closed {
			return brokerpkg.Event{}, io.EOF
		}

		// Nothing new after a poll: wait for either a change (another
		// caller's Submit/Cancel/Replace against the same account —
		// though see ADR-051's own note that Cancel/Replace do not
		// themselves signal this; only a synthesized event from a poll
		// or Submit does, so a reader waiting here after Cancel/Replace
		// wakes at the next poll interval, not immediately), this
		// reader's own Close, or the next poll interval, honoring ctx
		// throughout.
		timer := r.account.broker.deps.Clock.NewTimer(r.account.broker.deps.pollInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return brokerpkg.Event{}, ctx.Err()
		case <-r.done:
			timer.Stop()
			return brokerpkg.Event{}, brokerpkg.ErrClosed
		case <-changed:
			timer.Stop()
		case <-timer.C():
		}
	}
}

func (r *eventReader) isClosed() bool {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	return r.closed
}

// Close implements broker.EventReader. It releases no resources of its
// own (there is no background goroutine or open connection to stop —
// polling only ever happens synchronously inside a caller's own Next
// call), but marks the reader unusable and wakes a Next currently
// blocked in its own select (via done), matching the interface's
// "Close is safe to call more than once and safe to call concurrently
// with a blocked Next" contract for real, not merely by having nothing
// left to block on.
func (r *eventReader) Close() error {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	close(r.done)
	return nil
}

// poll fetches every order Alpaca reports since this account's last
// poll baseline, diffs each against the correlator's own last-observed
// (status, filled quantity), and appends whatever changed as new
// events. It fetches unconditionally from the beginning of the
// correlator's own tracked orders (Status: "all", no After bound)
// rather than tracking a separate wall-clock poll baseline: Alpaca's
// order list is small enough in Phase 1's paper-trading scope that
// re-listing everything each poll is simpler and cannot miss an update,
// at the cost of O(open-account-orders) work per poll rather than
// O(recently-changed) — an acceptable, documented tradeoff for Phase 1.
func (r *eventReader) poll(ctx context.Context) error {
	wireOrders, err := r.account.broker.client.ListOrders(ctx, ListOrdersOptions{Status: "all"})
	if err != nil {
		return fmt.Errorf("alpaca: poll orders: %w", err)
	}

	for _, wo := range wireOrders {
		newState := observedOrderState{status: wo.Status, filledQty: orDefault(wo.FilledQty, "0")}
		prev := r.account.broker.corr.observe(wo.ID, newState)
		if prev == newState {
			continue
		}

		o, err := wireOrderToOrder(wo, r.account.broker.deps.Resolver, r.account.broker.name, r.account.broker.ref.AccountID, r.account.broker.deps.IDs)
		if err != nil {
			return fmt.Errorf("alpaca: poll orders: %w", err)
		}
		r.account.broker.corr.rememberOrderID(o.Request.OrderID, wo.ID)

		if _, err := r.account.broker.corr.appendEvent(func(sequence uint64) (brokerpkg.Event, error) {
			return buildOrderEvent(r.account.broker.deps, o, id.EventID{}, sequence)
		}); err != nil {
			return fmt.Errorf("alpaca: poll orders: record order event: %w", err)
		}

		if err := r.emitFillIfIncreased(prev, newState, o); err != nil {
			return err
		}
	}
	return nil
}

// emitFillIfIncreased synthesizes and records an EventKindFill event
// when newState's filled quantity is greater than prev's — see the
// package doc comment for why this uses the order's cumulative
// filled_avg_price as the increment's price, an approximation that is
// exact for a single-execution fill and only approximate across
// multiple partial executions observed between two polls.
func (r *eventReader) emitFillIfIncreased(prev, newState observedOrderState, o order.Order) error {
	prevQty, err := num.ParseQuantity(orDefault(prev.filledQty, "0"))
	if err != nil {
		return fmt.Errorf("alpaca: parse previous filled qty: %w", err)
	}
	newQty, err := num.ParseQuantity(newState.filledQty)
	if err != nil {
		return fmt.Errorf("alpaca: parse new filled qty: %w", err)
	}
	if newQty.Cmp(prevQty) <= 0 {
		return nil
	}
	delta, err := newQty.Sub(prevQty)
	if err != nil {
		return fmt.Errorf("alpaca: compute fill delta: %w", err)
	}
	if o.AvgFillPrice == nil {
		return fmt.Errorf("alpaca: order %s reports increased filled quantity with no fill price", o.BrokerOrderID)
	}

	fillID, err := id.GenerateFillID(r.account.broker.deps.IDs)
	if err != nil {
		return err
	}
	fill, err := order.NewFill(order.Fill{
		FillID:        fillID,
		OrderID:       o.Request.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		AccountID:     o.Request.AccountID,
		Listing:       o.Request.Listing,
		Side:          o.Request.Side,
		Price:         *o.AvgFillPrice,
		Quantity:      delta,
		Timestamp:     r.account.broker.deps.Clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("alpaca: build synthesized fill: %w", err)
	}

	_, err = r.account.broker.corr.appendEvent(func(sequence uint64) (brokerpkg.Event, error) {
		eventID, err := id.GenerateEventID(r.account.broker.deps.IDs)
		if err != nil {
			return brokerpkg.Event{}, err
		}
		now := r.account.broker.deps.Clock.Now()
		fill.Metadata = id.Metadata{EventID: eventID, Timestamp: now}
		return brokerpkg.NewEvent(brokerpkg.Event{
			Metadata:   id.Metadata{EventID: eventID, Timestamp: now},
			ObservedAt: now,
			Sequence:   sequence,
			Kind:       brokerpkg.EventKindFill,
			Fill:       &fill,
		})
	})
	return err
}
