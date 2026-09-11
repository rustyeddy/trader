package execution

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// ErrInvalidRequest marks a request that is missing a service-level
// required field. It is never returned alongside a partial result.
//
// Validate checks only what this service layer itself must have to
// invoke the use case (a non-zero AccountID) — it does not duplicate
// order.NewIntent, execution's PlanInput checks, or risk's SizeInput/
// Input checks; those remain owned by order, execution, risk, and
// pipeline, and their own classifiable sentinels propagate through
// Submit unwrapped-checkable, per #186's own review (avoid duplicating
// domain validation another layer already owns).
var ErrInvalidRequest = errors.New("service/execution: invalid request")

// ErrBracketNotSupported reports that req.Intent.Kind is
// order.IntentEnterWithStop (issue #351, ADR-059). SubmitResponse has
// no field carrying pipeline.Result.Bracket — silently discarding a
// successful bracket outcome would be worse than rejecting it
// outright (PR #367 review) — and ADR-059's own atomicity guarantee is
// explicitly backtest-only (it works only against a broker that fills
// a Market order synchronously inside Submit, today only
// adapters/broker/sim), which this service layer cannot assume for
// whatever broker its own caller configured. backtest.Scheduler is
// the one caller that submits this intent kind today, and it talks to
// *pipeline.Pipeline directly, never through this service.
var ErrBracketNotSupported = errors.New("service/execution: order.IntentEnterWithStop is not yet supported through this service (issue #366)")

// SubmitRequest is the request for the Submit use case: carry req.Intent
// through the full canonical M4 path against req.AccountID's current,
// freshly-fetched account state.
type SubmitRequest struct {
	// AccountID identifies the account to size, plan, risk-evaluate,
	// and submit against. Required.
	AccountID id.AccountID

	// Intent is what a strategy or operator wants to accomplish.
	Intent order.Intent

	// Listing is the concrete, venue-specific instrument Intent
	// targets. See pipeline.Input.Listing's own doc comment.
	Listing instrument.Listing

	// RiskFraction is the fraction of account equity Sizer may risk
	// when sizing an unsized order.IntentEnter. See
	// pipeline.Input.RiskFraction.
	RiskFraction num.Rate

	// AdverseDistance is the adverse price-distance assumption this
	// intent is sized/admitted against. See
	// pipeline.Input.AdverseDistance.
	AdverseDistance *num.Price

	// ReferencePrice is the valuation price threaded into risk
	// admission for a value-based Rule. See
	// pipeline.Input.ReferencePrice.
	ReferencePrice *num.Price
}

// Validate reports whether r is well-formed enough to attempt,
// returning a wrapped ErrInvalidRequest for the first problem found.
func (r SubmitRequest) Validate() error {
	if r.AccountID.IsZero() {
		return fmt.Errorf("%w: account id is zero", ErrInvalidRequest)
	}
	if r.Intent.Kind == order.IntentEnterWithStop {
		return ErrBracketNotSupported
	}
	return nil
}
