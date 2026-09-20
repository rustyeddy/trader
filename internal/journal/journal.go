// Package journal defines the storage-neutral contract for a
// durable, replayable record of what happened during a run (issue
// #218, M5-10; ADR-036). It owns only the semantic Entry/Record model
// and validation — concrete storage adapters (for example the JSONL
// implementation in adapters/journal/jsonl) are peers, not part of
// this package, so a future SQLite/Postgres/etc. adapter never widens
// this package's own surface.
//
// journal depends only on id, num, instrument, order, risk, broker,
// and account — never on pipeline or backtest — so it remains
// reusable by a future live session exactly as ADR-035 anticipated,
// not backtest-specific.
package journal

import "context"

// Recorder durably appends Records, assigning each one's canonical
// Sequence itself (see Record's own doc comment for why a caller
// cannot supply or influence it). Implementations must preserve call
// order exactly: Record N's assigned Sequence is always less than
// Record N+1's, for any two successful calls on the same Recorder.
//
// A Recorder is not required to make a Record durable before Record
// returns — see a concrete implementation's own doc comment for its
// durability guarantees — but it must never silently drop one: a
// failure to durably record is always a returned error, never a
// discarded write.
type Recorder interface {
	// Record durably appends rec, in call order. A caller-supplied
	// rec.Metadata.Timestamp business time is preserved; Sequence is
	// assigned internally and is not observable through this call —
	// use a Reader to read back the fully materialized Entry.
	Record(ctx context.Context, rec Record) error

	// Close releases any resources this Recorder holds. Close does not
	// imply ownership of any dependency the Recorder was constructed
	// with beyond its own storage handle — see the concrete
	// implementation's own doc comment.
	Close() error
}

// Reader streams a Recorder's own Entry values back in ascending
// Sequence order — the exact order they were recorded in, per
// Recorder's own ordering guarantee.
type Reader interface {
	// Next returns the next Entry in Sequence order. It returns io.EOF
	// once every entry the underlying journal currently holds has been
	// delivered.
	Next(ctx context.Context) (Entry, error)

	// Close releases resources this Reader holds.
	Close() error
}

// discard is a Recorder that durably records nothing. It is the
// default when a caller has no journal configured, matching the
// logging.Discard() convention already used elsewhere in Trader.
type discard struct{}

// Discard returns a Recorder whose Record calls always succeed and
// whose Close is a no-op. Use it as the default when a caller has not
// configured a real journal.
func Discard() Recorder { return discard{} }

func (discard) Record(ctx context.Context, rec Record) error { return nil }
func (discard) Close() error                                 { return nil }

var _ Recorder = discard{}
