package sim

import (
	"context"
	"io"
	"strconv"
	"sync"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/order"
)

// eventReader is a broker.EventReader over one accountState's in-memory
// event log. Next blocks for a future event rather than returning
// io.EOF merely because it has caught up (ADR-024): it returns io.EOF
// only once the owning Broker has been closed and every already-
// recorded event has been delivered — the producer itself has ended,
// matching Account.Events's documented contract.
type eventReader struct {
	state *accountState
	after uint64

	mu     sync.Mutex
	idx    int
	closed bool
}

var _ brokerpkg.EventReader = (*eventReader)(nil)
var _ brokerpkg.FiniteEventReader = (*eventReader)(nil)

// Next implements broker.EventReader.
func (r *eventReader) Next(ctx context.Context) (brokerpkg.Event, error) {
	for {
		select {
		case <-ctx.Done():
			return brokerpkg.Event{}, ctx.Err()
		default:
		}

		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return brokerpkg.Event{}, brokerpkg.ErrClosed
		}
		r.mu.Unlock()

		r.state.mu.Lock()
		for r.idx < len(r.state.events) {
			e := r.state.events[r.idx]
			r.idx++
			if e.Sequence > r.after {
				r.state.mu.Unlock()
				return cloneEvent(e), nil
			}
		}
		accountClosed := r.state.closed
		waitCh := r.state.changed
		r.state.mu.Unlock()

		if accountClosed {
			return brokerpkg.Event{}, io.EOF
		}

		select {
		case <-ctx.Done():
			return brokerpkg.Event{}, ctx.Err()
		case <-waitCh:
			// A new event was appended, or the account just closed;
			// loop back and re-check both under the lock.
		}
	}
}

// AtEnd implements broker.FiniteEventReader: it reports whether every
// currently recorded event has already been delivered, without
// consuming one or blocking. It peeks the same underlying slice Next
// itself walks (from r.idx onward, skipping anything at or before
// r.after) but never advances r.idx, so it never changes what a
// subsequent Next call returns.
func (r *eventReader) AtEnd() bool {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	for i := r.idx; i < len(r.state.events); i++ {
		if r.state.events[i].Sequence > r.after {
			return false
		}
	}
	return true
}

// Close implements broker.EventReader. It is safe to call more than
// once, and safe to call concurrently with a blocked Next: a reader
// blocked waiting on waitCh in Next is not directly interrupted by
// Close, but Close's own next call to Next will observe r.closed and
// return promptly. A caller that needs an in-flight Next to return
// immediately on Close should cancel the context it passed to Next —
// the same pattern the package doc comment recommends generally.
func (r *eventReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// cloneEvent returns a copy of e whose payload pointer shares no
// mutable state with e: a caller that mutates the returned Event's
// Order/Fill/Status fields must never be able to corrupt this
// accountState's own recorded event log. Account is not deep-cloned:
// account.Snapshot is already an immutable value with no exported
// mutator and no exported way to reach its unexported fields, so
// sharing the pointer is safe.
func cloneEvent(e brokerpkg.Event) brokerpkg.Event {
	cloned := e
	if e.Order != nil {
		o := cloneOrder(*e.Order)
		cloned.Order = &o
	}
	if e.Fill != nil {
		f := cloneFill(*e.Fill)
		cloned.Fill = &f
	}
	if e.Status != nil {
		s := *e.Status
		cloned.Status = &s
	}
	return cloned
}

// cloneFill returns a copy of f that shares no pointer state with it —
// only Commission needs it, since num.Money/num.Price/num.Quantity are
// themselves plain immutable values with no pointer fields.
func cloneFill(f order.Fill) order.Fill {
	cloned := f
	if f.Commission != nil {
		v := *f.Commission
		cloned.Commission = &v
	}
	return cloned
}

// decodeCursor decodes an EventCursor produced by encodeCursor. An
// empty or malformed cursor decodes to 0, meaning "replay from the
// beginning" — EventCursor's zero value is always legal, never an
// error (ADR-024).
func decodeCursor(c brokerpkg.EventCursor) uint64 {
	if c == "" {
		return 0
	}
	seq, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// encodeCursor encodes seq as an EventCursor a later Account.Events
// call can resume from.
func encodeCursor(seq uint64) brokerpkg.EventCursor {
	return brokerpkg.EventCursor(strconv.FormatUint(seq, 10))
}
