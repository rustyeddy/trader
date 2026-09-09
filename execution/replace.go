package execution

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
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
// requirements PlanReplace itself still enforces. Mirrors
// checkPlanInput's own validation exactly (PR #337 review: an earlier
// version of this function skipped the Account-construction and
// Listing/Account provider-consistency checks checkPlanInput already
// enforces, so calling PlanReplace directly — bypassing
// pipeline.Pipeline, which happens to validate this itself before ever
// reaching PlanReplace — could misclassify a genuinely invalid input as
// ErrNoRestingStopOrder, or let a provider mismatch slip through
// unnoticed).
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
	if in.Account.AccountID().IsZero() {
		return order.Intent{}, fmt.Errorf("%w: account must be constructed", ErrInvalidPlanInput)
	}
	if !strings.EqualFold(in.Listing.Provider(), in.Account.Broker()) {
		return order.Intent{}, fmt.Errorf("%w: listing provider %q does not match account broker %q",
			ErrInvalidPlanInput, in.Listing.Provider(), in.Account.Broker())
	}
	return intent, nil
}

// PlanReplace implements Planner: it finds the account's own resting
// protective Stop order for in.Listing's instrument (findRestingStopOrder's
// own doc comment defines exactly what qualifies) and returns a
// validated order.ReplaceRequest moving that order's StopPrice to
// in.Intent's — ratcheting an already-placed protective stop, as
// opposed to Plan's own IntentAdjustStop case, which places the
// *initial* one (there is nothing yet for PlanReplace to find).
// ErrNoRestingStopOrder reports the case Plan is meant to handle
// instead.
//
// PlanReplace also keeps the stop's own Quantity synchronized with the
// account's current position size in in.Listing's instrument, setting
// NewQuantity whenever it has changed since the resting order was last
// placed or replaced (PR #337 review — issue #336's own non-goals
// already scoped this in: "quantity-changing replaces *beyond* what's
// needed to keep a protective stop's quantity in sync with the current
// position size" are out of scope; staying in sync is not). This is
// the one quantity-changing case PlanReplace supports — it does not
// otherwise plan independent, position-sizing-driven replacement.
// findRestingStopOrder's own side-matching already guarantees the
// order this function finds still actually protects the current
// position's side, so no separate side check is needed here.
//
// Beyond that, PlanReplace supports only order.IntentAdjustStop — see
// ADR-054 for why arbitrary limit-price replacement is not planned
// here: this package's only real consumer (a trailing-stop-style
// strategy) never needs it, and inventing unused replacement
// vocabulary speculatively is exactly what execution/doc.go's own
// history (the original, narrower IntentAdjustStop deferral this
// issue closes) warns against repeating.
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

	// Keep the stop's own Quantity synchronized with the current
	// position size (PR #337 review). planAdjustStop cannot fail here
	// with ErrNoPositionToProtect: findRestingStopOrder already found a
	// protective stop whose Side matches a real position's protective
	// side, so that same position is necessarily still open.
	_, currentQty, err := planAdjustStop(in.Account, in.Listing)
	if err != nil {
		return ReplaceResult{}, err
	}
	var newQuantity *num.Quantity
	if existing.AcceptedQuantity == nil || !existing.AcceptedQuantity.Equal(currentQty) {
		newQuantity = &currentQty
	}

	eventID, err := id.GenerateEventID(p.deps.IDs)
	if err != nil {
		return ReplaceResult{}, err
	}

	req, err := order.NewReplaceRequest(order.ReplaceRequest{
		OrderID:      existing.Request.OrderID,
		NewQuantity:  newQuantity,
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
