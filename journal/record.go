package journal

import (
	"encoding/json"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// RunStarted is the payload of a run's first journal record. Header is
// an opaque, caller-supplied blob — typically a marshaled
// backtest.Manifest — so this package never needs to depend on
// backtest or know what a "manifest" is; it just carries whatever
// reproducibility header the caller already produced.
type RunStarted struct {
	RunID  id.RunID
	Header json.RawMessage
}

// RunCompleted is the payload of a run's last journal record, written
// only once the run finished successfully. EntryCount is the number of
// records written before this one, letting a reader distinguish "the
// run completed" from "the file ends here because something stopped
// short" independent of the JSONL syntax itself being well-formed.
type RunCompleted struct {
	RunID      id.RunID
	EntryCount uint64
}

// Record is the unsequenced payload a caller submits to a Recorder.
// Record deliberately carries no Sequence: assigning the canonical,
// strictly-increasing journal order is the Recorder's own
// responsibility (see Recorder's doc comment), not something a caller
// can supply or disagree with. A Reader only ever produces the fully
// materialized Entry (Record plus the Sequence a Recorder assigned
// it).
//
// Exactly one of the payload fields is populated, matching Kind — the
// same discriminated-envelope shape broker.Event already established.
type Record struct {
	RunID    id.RunID
	Metadata id.Metadata
	Kind     Kind

	RunStarted   *RunStarted
	Intent       *order.Intent
	Proposal     *order.Proposal
	Decision     *risk.Decision
	Request      *order.Request
	Order        *order.Order
	Fill         *order.Fill
	Account      *account.Snapshot
	Status       *broker.Status
	Trade        *order.Trade
	RunCompleted *RunCompleted
}

// NewRecord validates and returns a Record. RunID must be non-zero,
// Kind must be one of its defined values, Metadata.Timestamp must be
// non-zero, and exactly one payload field must be set, matching Kind.
func NewRecord(r Record) (Record, error) {
	if r.RunID.IsZero() {
		return Record{}, fmt.Errorf("%w: run id must be set", ErrInvalidRecord)
	}
	if !r.Kind.valid() {
		return Record{}, fmt.Errorf("%w: invalid kind %v", ErrInvalidRecord, r.Kind)
	}
	if r.Metadata.Timestamp.IsZero() {
		return Record{}, fmt.Errorf("%w: metadata timestamp must be set", ErrInvalidRecord)
	}

	populated := 0
	if r.RunStarted != nil {
		populated++
	}
	if r.Intent != nil {
		populated++
	}
	if r.Proposal != nil {
		populated++
	}
	if r.Decision != nil {
		populated++
	}
	if r.Request != nil {
		populated++
	}
	if r.Order != nil {
		populated++
	}
	if r.Fill != nil {
		populated++
	}
	if r.Account != nil {
		populated++
	}
	if r.Status != nil {
		populated++
	}
	if r.Trade != nil {
		populated++
	}
	if r.RunCompleted != nil {
		populated++
	}
	if populated != 1 {
		return Record{}, fmt.Errorf("%w: exactly one payload must be set, found %d", ErrInvalidRecord, populated)
	}

	var ok bool
	switch r.Kind {
	case KindRunStarted:
		ok = r.RunStarted != nil
	case KindIntent:
		ok = r.Intent != nil
	case KindProposal:
		ok = r.Proposal != nil
	case KindDecision:
		ok = r.Decision != nil
	case KindRequest:
		ok = r.Request != nil
	case KindOrder:
		ok = r.Order != nil
	case KindFill:
		ok = r.Fill != nil
	case KindAccount:
		ok = r.Account != nil
	case KindStatus:
		ok = r.Status != nil
	case KindTrade:
		ok = r.Trade != nil
	case KindRunCompleted:
		ok = r.RunCompleted != nil
	}
	if !ok {
		return Record{}, fmt.Errorf("%w: kind %s does not match the populated payload", ErrInvalidRecord, r.Kind)
	}

	// RunStarted/RunCompleted each carry their own RunID field,
	// duplicating the envelope's — they are the journal's own
	// integrity/completion markers, so a mismatch here would produce an
	// internally contradictory but otherwise valid-looking entry rather
	// than a detectable error.
	if r.RunStarted != nil && !r.RunStarted.RunID.Equal(r.RunID) {
		return Record{}, fmt.Errorf("%w: run_started run id %s does not match record run id %s", ErrInvalidRecord, r.RunStarted.RunID, r.RunID)
	}
	if r.RunCompleted != nil && !r.RunCompleted.RunID.Equal(r.RunID) {
		return Record{}, fmt.Errorf("%w: run_completed run id %s does not match record run id %s", ErrInvalidRecord, r.RunCompleted.RunID, r.RunID)
	}

	return r, nil
}
