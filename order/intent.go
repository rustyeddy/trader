package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// IntentKind identifies which kind of trading intent an Intent expresses
// (ADR-005). It is a closed, Trader-controlled vocabulary: nothing
// external reports a kind Trader doesn't already know about, so there
// is no "unknown" sentinel — construction sites reject anything outside
// the defined values, matching Side's own convention.
//
// Only the four kinds ADR-005 itself names are defined for v0. A
// cancel-related intent, or a "no action taken" record, may be added
// later once a concrete strategy/execution use case defines what it
// would mean — see the M4-02 design discussion on issue #177.
type IntentKind uint8

const (
	intentUnset IntentKind = iota

	// IntentEnter expresses "open or increase a position in Side's
	// direction." It carries no Quantity — sizing is risk's
	// responsibility (ADR-006), not the intent's.
	IntentEnter

	// IntentExit expresses "remove whatever exposure currently exists
	// for Instrument," in whichever direction that happens to be. It
	// carries no Side or Quantity: the strategy does not need to know
	// the current position's direction or size to ask for it to be
	// closed.
	IntentExit

	// IntentAdjustStop expresses "move Instrument's protective stop to
	// StopPrice," an absolute price, not a broker-native trailing or
	// offset instruction.
	IntentAdjustStop

	// IntentTargetExposure expresses "reach exactly this position,"
	// carrying the desired absolute Side and Quantity rather than a
	// delta from whatever position currently exists.
	IntentTargetExposure

	// IntentEnterWithStop expresses "open a position in Side's
	// direction, and ensure it has a protective stop at StopPrice
	// active from the same fill" (issue #351, ADR-059) — a bracket
	// entry, closing the gap where a strategy's own first protective
	// stop (emitted only once the position is observed open, one bar
	// after the fill) leaves the entry bar itself completely
	// unprotected. Like IntentEnter, it carries no Quantity — sizing
	// remains risk's responsibility (ADR-006). StopPrice must be
	// computable before the fill (for example from an indicator value,
	// never from the real average fill price), since the two legs this
	// intent expands into are submitted before that fill price is
	// known to the caller.
	//
	// pipeline.Pipeline is the only place this intent is actually
	// fulfilled (see its own Submit/submitBracket doc comments):
	// execution.Planner never sees IntentEnterWithStop directly, and
	// Pipeline.Evaluate rejects it outright, since verifying the
	// second (stop) leg's own viability requires a real broker
	// submission for the first (entry) leg — a bracket cannot be
	// evaluated read-only. ADR-059 records this decision's own
	// deliberate scope: it succeeds only against a broker that fills a
	// Market order synchronously inside Submit (today, only
	// adapters/broker/sim) and fails loudly, not silently, against one
	// that does not (every real broker adapter today) — see
	// pipeline.ErrBracketEntryNotSynchronouslyFilled.
	IntentEnterWithStop
)

// String returns a human-readable IntentKind name.
func (k IntentKind) String() string {
	switch k {
	case IntentEnter:
		return "enter"
	case IntentExit:
		return "exit"
	case IntentAdjustStop:
		return "adjust_stop"
	case IntentTargetExposure:
		return "target_exposure"
	case IntentEnterWithStop:
		return "enter_with_stop"
	default:
		return fmt.Sprintf("IntentKind(%d)", uint8(k))
	}
}

func (k IntentKind) valid() bool {
	switch k {
	case IntentEnter, IntentExit, IntentAdjustStop, IntentTargetExposure, IntentEnterWithStop:
		return true
	default:
		return false
	}
}

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
	Kind IntentKind

	// Instrument identifies the economic instrument this intent
	// concerns.
	Instrument instrument.ID

	// Side is required for IntentEnter, IntentTargetExposure, and
	// IntentEnterWithStop, and must be the zero value otherwise.
	Side Side

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
	if !in.Kind.valid() {
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
	requireSide := in.Kind == IntentEnter || in.Kind == IntentTargetExposure || in.Kind == IntentEnterWithStop
	requireQuantity := in.Kind == IntentTargetExposure
	requireStopPrice := in.Kind == IntentAdjustStop || in.Kind == IntentEnterWithStop

	if requireSide {
		if !in.Side.valid() {
			return fmt.Errorf("side must be set for %v", in.Kind)
		}
	} else if in.Side != sideUnset {
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
