package execution

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/order"
)

// ReplaceInput is what PlanReplace needs to plan replacing an
// existing resting order (issue #336). Unlike PlanInput, it carries no
// Quantity: PlanReplace's only supported Intent.Kind (IntentAdjustStop)
// never changes an order's quantity, only its StopPrice — quantity
// stays whatever the existing order already has.
type ReplaceInput struct {
	// Intent is what a strategy or operator wants to accomplish. Kind
	// must be order.IntentAdjustStop; PlanReplace supports no other
	// kind (ADR-054).
	Intent order.Intent

	// Listing is the concrete, venue-specific instrument this intent
	// targets — see PlanInput.Listing's own doc comment for the
	// Instrument/Provider consistency this must satisfy.
	Listing instrument.Listing

	// Account is the current, authoritative account state PlanReplace
	// finds the resting order to replace in.
	Account account.Snapshot
}

// ReplaceResult is what PlanReplace produces: a validated
// order.ReplaceRequest naming the resting order to replace and its
// new StopPrice, not yet submitted to a broker or risk-evaluated.
type ReplaceResult struct {
	Request order.ReplaceRequest
}

// checkReplaceInput validates in's fields shared by every
// ReplaceInput consumer, independent of Intent.Kind-specific
// requirements PlanReplace itself still enforces.
func checkReplaceInput(in ReplaceInput) (order.Intent, error) {
	intent, err := order.NewIntent(in.Intent)
	if err != nil {
		return order.Intent{}, fmt.Errorf("%w: intent: %v", ErrInvalidPlanInput, err)
	}
	if in.Listing.InstrumentID().IsZero() {
		return order.Intent{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidPlanInput)
	}
	if !in.Listing.InstrumentID().Equal(intent.Instrument) {
		return order.Intent{}, fmt.Errorf("%w: listing instrument %s does not match intent instrument %s",
			ErrInvalidPlanInput, in.Listing.InstrumentID(), intent.Instrument)
	}
	return intent, nil
}

// PlanReplace implements Planner: it finds the account's own resting
// Stop order for in.Listing's instrument and returns a validated
// order.ReplaceRequest moving that order's StopPrice to in.Intent's —
// ratcheting an already-placed protective stop, as opposed to Plan's
// own IntentAdjustStop case, which places the *initial* one (there is
// nothing yet for PlanReplace to find). ErrNoRestingStopOrder reports
// the case Plan is meant to handle instead.
//
// PlanReplace supports only order.IntentAdjustStop — see ADR-054 for
// why quantity/limit-price replacement is not planned here: this
// package's only real consumer (a trailing-stop-style strategy) only
// ever moves a stop price, and inventing unused replacement vocabulary
// speculatively is exactly what execution/doc.go's own history (the
// original, narrower IntentAdjustStop deferral this issue closes)
// warns against repeating.
//
// PlanReplace shares Plan's determinism contract: given identical
// *initial* Deps state and an identical ReplaceInput, two independently
// constructed Planners produce identical ReplaceResult values,
// including Request.Metadata.EventID.
func (p *planner) PlanReplace(ctx context.Context, in ReplaceInput) (ReplaceResult, error) {
	if err := ctx.Err(); err != nil {
		return ReplaceResult{}, err
	}

	intent, err := checkReplaceInput(in)
	if err != nil {
		return ReplaceResult{}, err
	}
	if intent.Kind != order.IntentAdjustStop {
		return ReplaceResult{}, fmt.Errorf("%w: %v", ErrUnsupportedIntentKind, intent.Kind)
	}

	existing, ok := findRestingStopOrder(in.Account, in.Listing)
	if !ok {
		return ReplaceResult{}, fmt.Errorf("%w: %v", ErrNoRestingStopOrder, intent.Instrument)
	}

	eventID, err := id.GenerateEventID(p.deps.IDs)
	if err != nil {
		return ReplaceResult{}, err
	}

	req, err := order.NewReplaceRequest(order.ReplaceRequest{
		OrderID:      existing.Request.OrderID,
		NewStopPrice: intent.StopPrice,
		Metadata: id.Metadata{
			EventID:       eventID,
			CorrelationID: intent.Metadata.CorrelationID,
			CausationID:   intent.Metadata.EventID,
			Timestamp:     p.deps.Clock.Now(),
		},
	})
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("execution: building replace request: %w", err)
	}
	return ReplaceResult{Request: req}, nil
}
