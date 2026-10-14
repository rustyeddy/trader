package accountactivity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Errors reported by Store implementations and Batch/Query validation.
var (
	// ErrInvalidBatch reports a Batch that fails Validate.
	ErrInvalidBatch = errors.New("accountactivity: invalid batch")
	// ErrInvalidQuery reports a Query that fails Validate.
	ErrInvalidQuery = errors.New("accountactivity: invalid query")
	// ErrConflict reports a batch activity whose Key is already stored with
	// different content. The ledger is append-only (ADR-079): an activity is
	// never silently overwritten, so a conflict is surfaced for an operator
	// and the whole batch is rejected, leaving the store and checkpoint
	// untouched.
	ErrConflict = errors.New("accountactivity: stored activity differs from the imported one")
)

// SyncKey names one independently synchronized stream: a provider
// environment's account. It is the unit a checkpoint belongs to and a
// Query reads. It is comparable.
type SyncKey struct {
	Provider          string
	Environment       Environment
	ProviderAccountID string
}

// Validate checks that k names a stream: a lowercase provider, a valid
// environment and a provider account ID.
func (k SyncKey) Validate() error {
	if k.Provider == "" || k.Provider != strings.ToLower(strings.TrimSpace(k.Provider)) {
		return fmt.Errorf("provider must be a non-empty lowercase name")
	}
	if !k.Environment.Valid() {
		return fmt.Errorf("environment must be practice or live")
	}
	if k.ProviderAccountID == "" {
		return fmt.Errorf("provider account id must be set")
	}
	return nil
}

// String renders the key as provider/environment/account.
func (k SyncKey) String() string {
	return k.Provider + "/" + k.Environment.String() + "/" + k.ProviderAccountID
}

// SyncKey returns the stream a belongs to.
func (a Activity) SyncKey() SyncKey {
	return SyncKey{Provider: a.Provider, Environment: a.Environment, ProviderAccountID: a.ProviderAccountID}
}

// Checkpoint records how far a stream has been durably imported. Cursor is
// opaque to the store and to this package: its meaning (a transaction ID, a
// timestamp, a page token) belongs to the provider adapter, which keeps
// provider cursor semantics out of the canonical domain.
type Checkpoint struct {
	Sync   SyncKey
	Cursor string
	// UpdatedAt is when the checkpoint was written, supplied by the caller
	// from an injected clock; a Store never reads the clock.
	UpdatedAt time.Time
}

// Batch is one unit of import: the activities fetched in a step and the
// cursor to resume from once they are all stored.
//
// Apply ties the two together deliberately. There is no way to advance a
// checkpoint without the activities that precede it, so a caller cannot
// get the order wrong.
type Batch struct {
	Sync       SyncKey
	Activities []Activity
	// Cursor is the provider cursor to checkpoint after Activities are
	// durably stored. It must be non-empty.
	Cursor string
	// At is the checkpoint time, from an injected clock.
	At time.Time
}

// Validate checks the batch before anything is written. The returned error
// wraps ErrInvalidBatch.
func (b Batch) Validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidBatch, fmt.Sprintf(format, args...))
	}
	if err := b.Sync.Validate(); err != nil {
		return bad("sync key: %v", err)
	}
	if b.Cursor == "" {
		return bad("cursor must be set")
	}
	if b.At.IsZero() {
		return bad("at must be set (from an injected clock)")
	}
	for i, a := range b.Activities {
		if err := a.Validate(); err != nil {
			return bad("activity %d: %v", i, err)
		}
		if a.SyncKey() != b.Sync {
			return bad("activity %d belongs to %s, not %s", i, a.SyncKey(), b.Sync)
		}
	}
	return nil
}

// ApplyResult reports what Apply did with a batch's activities.
type ApplyResult struct {
	// Added is how many activities were new to the store.
	Added int
	// Unchanged is how many were already stored with identical content,
	// including repeats within the batch itself.
	Unchanged int
}

// Query selects one stream's activities in a half-open time range.
type Query struct {
	Sync SyncKey
	// From is inclusive and To exclusive, both on OccurredAt. A zero bound is
	// unbounded.
	From, To time.Time
	// Kinds, if non-empty, keeps only those kinds.
	Kinds []Kind
}

// Validate checks the query. The returned error wraps ErrInvalidQuery.
func (q Query) Validate() error {
	if err := q.Sync.Validate(); err != nil {
		return fmt.Errorf("%w: sync key: %v", ErrInvalidQuery, err)
	}
	if !q.From.IsZero() && !q.To.IsZero() && q.To.Before(q.From) {
		return fmt.Errorf("%w: to is before from", ErrInvalidQuery)
	}
	for _, k := range q.Kinds {
		if !k.Valid() {
			return fmt.Errorf("%w: kind filter has an invalid kind", ErrInvalidQuery)
		}
	}
	return nil
}

// Match reports whether a satisfies q's time range and kind filter. It does
// not check the stream; a Store reads only q.Sync's stream.
func (q Query) Match(a Activity) bool {
	if !q.From.IsZero() && a.OccurredAt.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && !a.OccurredAt.Before(q.To) {
		return false
	}
	if len(q.Kinds) == 0 {
		return true
	}
	for _, k := range q.Kinds {
		if a.Kind == k {
			return true
		}
	}
	return false
}

// Compare orders activities deterministically: by OccurredAt, then by
// ProviderActivityID, shorter before longer and otherwise lexicographic, so
// digit-string IDs (OANDA's) sort numerically. It returns -1, 0 or 1. The
// order is deterministic, not a claim about provider sequence beyond
// OccurredAt.
func Compare(a, b Activity) int {
	if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
		return c
	}
	if len(a.ProviderActivityID) != len(b.ProviderActivityID) {
		if len(a.ProviderActivityID) < len(b.ProviderActivityID) {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ProviderActivityID, b.ProviderActivityID)
}

// Store is the durable account-activity ledger and its sync checkpoints. It
// is storage-neutral, like journal's ports: adapters live under
// internal/adapters, and storetest holds the contract any of them must
// pass. A Store has no run or journal identity.
//
// The import path is fetch, normalize, Apply. Apply is idempotent and a
// failed Apply can be retried with the same Batch.
type Store interface {
	// Apply durably stores b.Activities, then advances the stream's
	// checkpoint to b.Cursor, and only then returns nil. Activities already
	// stored identically are left as they are (the first ImportedAt wins);
	// an activity whose Key exists with different content fails the whole
	// batch with ErrConflict and changes nothing. If Apply fails after
	// storing activities but before the checkpoint moves, the checkpoint is
	// unchanged and retrying the same Batch converges.
	Apply(ctx context.Context, b Batch) (ApplyResult, error)

	// Checkpoint returns the stream's checkpoint, or false if it has none.
	Checkpoint(ctx context.Context, k SyncKey) (Checkpoint, bool, error)

	// Query returns the stream's matching activities in Compare order.
	Query(ctx context.Context, q Query) ([]Activity, error)
}
