package accountactivity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
)

// DefaultListLimit bounds a listing when the caller names no limit, so an
// accidental unbounded query cannot flood a terminal.
const DefaultListLimit = 100

// ListParams selects activities from one stream.
type ListParams struct {
	Store  accountactivity.Store
	Stream accountactivity.SyncKey
	// From is inclusive and To exclusive, on OccurredAt; a zero bound is
	// unbounded.
	From, To time.Time
	// Kinds, if non-empty, keeps only those kinds.
	Kinds []accountactivity.Kind
	// Limit caps the result; zero means DefaultListLimit and a negative
	// value is refused. The earliest activities in the range are kept.
	Limit int
}

// ListResult is a bounded, ordered view of a stream.
type ListResult struct {
	Stream accountactivity.SyncKey
	// Activities are in ledger order, at most Limit of them.
	Activities []accountactivity.Activity
	// Matched is how many activities matched before the limit applied.
	Matched int
	// Limit is the limit that was applied.
	Limit int
	// Checkpoint is the stream's import checkpoint, absent before any sync.
	Checkpoint    accountactivity.Checkpoint
	HasCheckpoint bool
}

// Truncated reports whether the limit hid matching activities.
func (r ListResult) Truncated() bool { return r.Matched > len(r.Activities) }

// List returns the stream's activities in the range, in ledger order.
func List(ctx context.Context, p ListParams) (ListResult, error) {
	if p.Store == nil {
		return ListResult{}, errors.New("accountactivity: a store is required")
	}
	if p.Limit < 0 {
		return ListResult{}, fmt.Errorf("accountactivity: limit %d must not be negative", p.Limit)
	}
	limit := p.Limit
	if limit == 0 {
		limit = DefaultListLimit
	}
	all, err := p.Store.Query(ctx, accountactivity.Query{Sync: p.Stream, From: p.From, To: p.To, Kinds: p.Kinds})
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Stream: p.Stream, Matched: len(all), Limit: limit, Activities: all}
	if len(all) > limit {
		res.Activities = all[:limit]
	}
	if res.Activities == nil {
		res.Activities = []accountactivity.Activity{}
	}
	cp, ok, err := p.Store.Checkpoint(ctx, p.Stream)
	if err != nil {
		return ListResult{}, err
	}
	res.Checkpoint, res.HasCheckpoint = cp, ok
	return res, nil
}
