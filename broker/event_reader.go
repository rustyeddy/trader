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
// implement, giving a caller a deterministic way to drain exactly the
// events its producer had already recorded when the reader was
// created — a completed backtest run, for example — without racing
// Next's ordinary live-stream blocking behavior or guessing "caught
// up" from elapsed time.
//
// The capability's contract is a fixed high-water mark, not a live
// "anything queued right now" check: an implementation captures, at
// the moment the reader is created (Account.Events(ctx, cursor)
// returns it), the highest Sequence its producer has recorded so far.
// AtEnd reports whether every event up to and including that captured
// boundary has been delivered by Next. Once AtEnd reports true, it
// stays true for the remaining lifetime of that reader, regardless of
// anything the producer records afterward — those later events are
// simply outside the boundary this reader captured at creation, not
// events this reader claims don't exist. This is what makes AtEnd
// usable as a real completeness proof rather than merely today's
// snapshot of an empty buffer: a caller draining until AtEnd is true
// is guaranteed to have seen every event recorded as of the moment it
// opened the reader, never a partial view that depends on how quickly
// it happened to drain relative to the producer.
//
// Next's own behavior is unchanged by a reader also implementing this
// capability: it keeps serving events indefinitely, live, exactly as
// an ordinary EventReader does — a caller that keeps calling Next past
// the point AtEnd first reported true still receives any event the
// producer records later. FiniteEventReader only adds a way to ask
// "have I seen everything from before I started," not a way to stop
// the underlying stream from producing more.
//
// This is deliberately a narrow, additive capability (matching the
// architecture document's "small required core plus capability
// discovery" guidance) rather than a change to EventReader's own
// Next/Close contract: an implementation backing a genuinely live or
// paper session simply does not implement it, and every existing
// EventReader consumer is unaffected.
type FiniteEventReader interface {
	EventReader

	// AtEnd reports whether every event recorded by this reader's
	// producer as of this reader's own creation has already been
	// delivered by Next. It performs no blocking I/O and does not
	// advance the reader's own position: it is safe to call repeatedly,
	// including between calls to Next. See FiniteEventReader's own doc
	// comment for why this is a fixed boundary captured at creation,
	// not a live re-evaluation of the producer's current backlog.
	AtEnd() bool
}
