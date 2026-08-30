package broker

import "context"

// EventCursor resumes an Account's event stream strictly after the
// event it represents: Account.Events(ctx, cursor) never redelivers an
// Event whose Sequence is <= a correctly persisted cursor. It is
// opaque, matching account.Snapshot.Cursor (ADR-019): callers carry it
// but do not interpret it. The zero EventCursor ("") means "start from
// the beginning of whatever backlog this Broker/Account retains" — an
// empty cursor is always legal, never an error. See Event's doc comment
// for how at-least-once delivery still arises despite this guarantee —
// it lives in the consumer's own cursor-persistence timing, not in this
// contract.
type EventCursor string

// EventReader streams an Account's Event values in ascending Sequence
// order (see Event's doc comment for the full ordering, duplicate, and
// out-of-order delivery contract). Obtain one from Account.Events.
//
// # Lifecycle and ownership
//
// The caller that obtains an EventReader from Account.Events owns
// closing it: no other code closes it on the caller's behalf. Close is
// safe to call more than once and safe to call concurrently with a
// blocked Next, which must then return promptly with an error rather
// than block indefinitely.
//
// # Backpressure
//
// Next does not return until an Event is available, ctx is canceled or
// expires, or the stream ends. A slow consumer therefore simply leaves
// events queued at the producer rather than causing them to be dropped
// or coalesced — see Event's doc comment for why critical events are
// never silently discarded. An implementation backed by a bounded
// buffer must document its own buffering/blocking behavior when the
// buffer is full, consistent with the architecture document's
// concurrency-ownership rules; this interface does not itself impose a
// buffer size.
type EventReader interface {
	// Next returns the next Event in Sequence order. It returns io.EOF
	// once the stream has delivered every event the producer currently
	// expects to produce — for example, a finished, bounded backtest
	// run. A live or paper broker's stream is expected to block via ctx
	// instead of returning io.EOF merely because no new event has
	// arrived yet; io.EOF from such a stream means the producer itself
	// has ended (for example, Broker.Close was called), not "caught
	// up." If ctx is canceled or expires before an Event is available,
	// Next returns ctx.Err().
	Next(ctx context.Context) (Event, error)

	// Close releases resources this EventReader holds. See the package
	// doc comment above for ownership, idempotency, and concurrent-call
	// requirements.
	Close() error
}

// FiniteEventReader is an optional capability an EventReader may
// implement when its underlying stream is backed by a bounded,
// currently-finished producer — a completed backtest run, for
// example — rather than a live/paper feed that can always receive
// more events later. A caller that needs to drain exactly what has
// been recorded so far, without racing Next's ordinary live-stream
// blocking behavior, type-asserts for this capability first.
//
// This is deliberately a narrow, additive capability (matching the
// architecture document's "small required core plus capability
// discovery" guidance) rather than a change to EventReader's own
// Next/Close contract: an implementation backing a genuinely live or
// paper session simply does not implement it, and every existing
// EventReader consumer is unaffected.
type FiniteEventReader interface {
	EventReader

	// AtEnd reports whether every event this reader's producer has
	// recorded so far has already been delivered by Next. It performs
	// no blocking I/O and does not advance the reader's own position:
	// it is safe to call repeatedly, including between calls to Next.
	// AtEnd returning true is not a permanent guarantee for a producer
	// that could still record more later (see FiniteEventReader's own
	// doc comment) — it answers "is there anything to read right now,"
	// not "will there ever be more."
	AtEnd() bool
}
