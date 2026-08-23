package sim

import (
	"context"
	"io"
	"strconv"
	"sync"

	brokerpkg "github.com/rustyeddy/trader/broker"
)

// eventReader is a broker.EventReader over one accountState's in-memory
// event log. It is bounded/replay-only: Next returns io.EOF once every
// currently recorded event has been delivered, matching the "finished,
// bounded run" case Account.Events's contract describes — this package
// has nothing that produces events asynchronously yet (see the package
// doc comment), so there is nothing for Next to block waiting on.
type eventReader struct {
	state *accountState
	after uint64

	mu     sync.Mutex
	idx    int
	closed bool
}

var _ brokerpkg.EventReader = (*eventReader)(nil)

// Next implements broker.EventReader.
func (r *eventReader) Next(ctx context.Context) (brokerpkg.Event, error) {
	select {
	case <-ctx.Done():
		return brokerpkg.Event{}, ctx.Err()
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-check ctx after acquiring r.mu: the pre-lock check above only
	// catches cancellation that happened before Next was called.
	select {
	case <-ctx.Done():
		return brokerpkg.Event{}, ctx.Err()
	default:
	}

	if r.closed {
		return brokerpkg.Event{}, brokerpkg.ErrClosed
	}

	r.state.mu.Lock()
	events := r.state.events
	r.state.mu.Unlock()

	for r.idx < len(events) {
		e := events[r.idx]
		r.idx++
		if e.Sequence > r.after {
			return e, nil
		}
	}
	return brokerpkg.Event{}, io.EOF
}

// Close implements broker.EventReader. It is safe to call more than
// once.
func (r *eventReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
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
