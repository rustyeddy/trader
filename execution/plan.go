package execution

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// PlanInput is what Planner.Plan needs to translate one order.Intent
// into an order.Proposal. Every field is supplied by the caller —
// Planner never fetches its own account state, market data, clock, or
// listing resolution; those are composition-root concerns (issue #179,
// M4-04's own design discussion).
type PlanInput struct {
	// Intent is what a strategy or operator wants to accomplish.
	Intent order.Intent

	// Listing is the concrete, venue-specific instrument execution
	// planning targets. Intent itself never carries one — it names an
	// instrument.ID, the canonical economic identity (#177) — so the
	// caller supplies the Listing execution planning should use.
	// Listing.InstrumentID() must equal Intent.Instrument, and
	// Listing.Provider() must case-insensitively equal Account.Broker()
	// — the same provider/broker consistency account.Snapshot itself
	// already enforces for every Position and OpenOrder it carries.
	Listing instrument.Listing

	// Account is the current, authoritative account state Plan
	// evaluates Intent against — in particular Account.Positions(),
	// which IntentExit and IntentTargetExposure both read to determine
	// what they are acting against.
	Account account.Snapshot

	// Quantity is the sizing decision an upstream risk sizing policy
	// has already made (ADR-006: sizing lives in risk, not execution)
	// for an intent that does not carry its own. Required, and must be
	// positive, exactly when Intent.Kind is order.IntentEnter; must be
	// nil otherwise — IntentExit derives its quantity from the current
	// position, and IntentTargetExposure already carries its own
	// desired Quantity on Intent itself.
	Quantity *num.Quantity
}

// PlanResult is what Planner.Plan produces for one PlanInput: exactly
// one order.Proposal, for the intent kinds this package supports.
// Bracket, OCO, and multi-leg planning — which could plausibly need
// more than one Proposal — are explicitly deferred by the architecture
// document; PlanResult is not shaped to anticipate them speculatively.
// When Plan cannot produce a Proposal (an unsupported Intent.Kind, no
// position to exit, an intent already satisfied), it returns a
// classifiable error instead of a zero-value PlanResult that could be
// mistaken for a real, empty proposal.
type PlanResult struct {
	Proposal order.Proposal
}

// Planner translates one order.Intent into a broker-neutral
// order.Proposal, without approving risk or submitting to a broker
// (ADR-005/ADR-006).
//
// Plan must be deterministic across independent Planner instances:
// given identical *initial* Deps state (the same starting clock time
// and ID-generator seed) and an identical PlanInput, two separately
// constructed Planners must produce identical PlanResult values,
// including the generated Proposal.Metadata.EventID. This is not a
// same-instance repeated-call guarantee — Deps.IDs is a stateful
// id.Generator, and each successful call legitimately advances it to
// produce a fresh EventID for a new event, the same way any other
// event-generating stage in this codebase behaves; two calls on the
// same Planner instance are expected to differ in EventID even for an
// otherwise-identical PlanInput. Plan must never consult time.Now,
// global randomness, or any state beyond what PlanInput and Deps
// supply.
//
// Plan enforces the intent -> proposal correlation chain ADR-005
// describes: the returned Proposal's Metadata.CorrelationID equals
// Intent.Metadata.CorrelationID, and Metadata.CausationID equals
// Intent.Metadata.EventID.
type Planner interface {
	Plan(ctx context.Context, in PlanInput) (PlanResult, error)
}

// checkPlanInput validates in's fields, independent of whether
// Intent.Kind is actually supported by a given Planner implementation
// (see planner.Plan's own per-kind dispatch for that). It revalidates
// Intent fully via order.NewIntent rather than trusting it by
// provenance — the same "each stage revalidates the one before it"
// rule order.Request/order.Order already apply to Proposal.
func checkPlanInput(in PlanInput) (order.Intent, error) {
	validIntent, err := order.NewIntent(in.Intent)
	if err != nil {
		return order.Intent{}, fmt.Errorf("%w: intent: %v", ErrInvalidPlanInput, err)
	}
	if in.Listing.InstrumentID().IsZero() {
		return order.Intent{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidPlanInput)
	}
	if !in.Listing.InstrumentID().Equal(validIntent.Instrument) {
		return order.Intent{}, fmt.Errorf("%w: listing instrument %s does not match intent instrument %s",
			ErrInvalidPlanInput, in.Listing.InstrumentID(), validIntent.Instrument)
	}
	if in.Account.AccountID().IsZero() {
		return order.Intent{}, fmt.Errorf("%w: account must be constructed", ErrInvalidPlanInput)
	}
	if !strings.EqualFold(in.Listing.Provider(), in.Account.Broker()) {
		return order.Intent{}, fmt.Errorf("%w: listing provider %q does not match account broker %q",
			ErrInvalidPlanInput, in.Listing.Provider(), in.Account.Broker())
	}

	requireQuantity := validIntent.Kind == order.IntentEnter
	switch {
	case requireQuantity && in.Quantity == nil:
		return order.Intent{}, fmt.Errorf("%w: quantity must be set for %v", ErrInvalidPlanInput, validIntent.Kind)
	case requireQuantity && in.Quantity.IsZero():
		return order.Intent{}, fmt.Errorf("%w: quantity must be positive for %v", ErrInvalidPlanInput, validIntent.Kind)
	case !requireQuantity && in.Quantity != nil:
		return order.Intent{}, fmt.Errorf("%w: quantity must not be set for %v", ErrInvalidPlanInput, validIntent.Kind)
	}

	return validIntent, nil
}
