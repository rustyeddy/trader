package order

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Intent is what a strategy or operator wants to accomplish for one
// instrument, before execution planning translates it into a concrete
// Proposal (ADR-005/ADR-006). It is broker-ignorant: Intent never
// appears in a broker-facing signature, and nothing about it assumes a
// particular adapter.
//
// Intent names an instrument.ID — the canonical economic identity
// (ADR-003) — not a venue-specific instrument.Listing. Listing carries
// venue mechanics (symbol, tick size, quantity increment, settlement
// currency) that are exactly what execution planning is responsible for
// applying (ADR-006); binding an Intent to one Listing this early would
// make the same strategy intent mean something different per broker.
// Execution planning selects or receives the concrete Listing as
// planning context when it turns an Intent into a Proposal.
//
// Field requirements are per Kind, not uniform — see NewIntent.
type Intent struct {
	// IntentID is Trader's own identifier for this intent, the first
	// stage of the intent -> proposal -> risk decision -> request/order
	// -> fill correlation chain (ADR-005).
	IntentID id.IntentID

	// Kind selects which of the four defined intents this value
	// expresses, and therefore which of Side/Quantity/StopPrice are
	// required, forbidden, or irrelevant. See NewIntent.
	Kind order.IntentKind

	// Instrument identifies the economic instrument this intent
	// concerns.
	Instrument instrument.ID

	// Side is required for IntentEnter, IntentTargetExposure, and
	// IntentEnterWithStop, and must be the zero value otherwise.
	Side order.Side

	// Quantity is required (and must be positive) for
	// IntentTargetExposure, and must be nil otherwise.
	Quantity *num.Quantity

	// StopPrice is required for IntentAdjustStop and
	// IntentEnterWithStop, and must be nil otherwise.
	StopPrice *num.Price

	// Metadata carries this intent's correlation and causation
	// context — the anchor for every later stage's own Metadata to
	// correlate back to.
	Metadata id.Metadata
}

// NewIntent validates and returns an Intent. IntentID and Instrument
// must be non-zero; Kind must be one of its defined values;
// Metadata.EventID and Metadata.CorrelationID must both be non-zero, so
// the intent -> proposal -> risk decision -> request/order -> fill
// correlation chain ADR-005 describes is anchored from the moment an
// Intent exists — Intent is the first stage of that chain, so nothing
// later can retroactively assign a CorrelationID an uncorrelatable
// Intent never had; and Side/Quantity/StopPrice must be present or
// zero/nil exactly as Kind requires:
//
//   - IntentEnter: Side required; Quantity and StopPrice forbidden.
//   - IntentExit: Side, Quantity, and StopPrice all forbidden.
//   - IntentAdjustStop: StopPrice required; Side and Quantity forbidden.
//   - IntentTargetExposure: Side and a positive Quantity required;
//     StopPrice forbidden.
//   - IntentEnterWithStop: Side and StopPrice required; Quantity
//     forbidden.
func NewIntent(in Intent) (Intent, error) {
	if err := checkIntent(in); err != nil {
		return Intent{}, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
	return in, nil
}

// checkIntent validates in's fields and returns a plain, unwrapped error
// describing the first problem found, or nil.
func checkIntent(in Intent) error {
	if in.IntentID.IsZero() {
		return fmt.Errorf("intent id must be set")
	}
	if in.Instrument.IsZero() {
		return fmt.Errorf("instrument must be set")
	}
	if !in.Kind.Valid() {
		return fmt.Errorf("invalid kind %v", in.Kind)
	}
	if in.Metadata.EventID.IsZero() {
		return fmt.Errorf("metadata event id must be set")
	}
	if in.Metadata.CorrelationID.IsZero() {
		return fmt.Errorf("metadata correlation id must be set")
	}
	return checkIntentKindFields(in)
}

func checkIntentKindFields(in Intent) error {
	requireSide := in.Kind == order.IntentEnter || in.Kind == order.IntentTargetExposure || in.Kind == order.IntentEnterWithStop
	requireQuantity := in.Kind == order.IntentTargetExposure
	requireStopPrice := in.Kind == order.IntentAdjustStop || in.Kind == order.IntentEnterWithStop

	if requireSide {
		if !in.Side.Valid() {
			return fmt.Errorf("side must be set for %v", in.Kind)
		}
	} else if in.Side != order.Side(0) {
		return fmt.Errorf("side must not be set for %v", in.Kind)
	}

	if requireQuantity {
		if in.Quantity == nil {
			return fmt.Errorf("quantity must be set for %v", in.Kind)
		}
		if in.Quantity.IsZero() {
			return fmt.Errorf("quantity must be positive for %v", in.Kind)
		}
	} else if in.Quantity != nil {
		return fmt.Errorf("quantity must not be set for %v", in.Kind)
	}

	if requireStopPrice {
		if in.StopPrice == nil {
			return fmt.Errorf("stop price must be set for %v", in.Kind)
		}
	} else if in.StopPrice != nil {
		return fmt.Errorf("stop price must not be set for %v", in.Kind)
	}

	return nil
}
