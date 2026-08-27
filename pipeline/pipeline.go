package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// Input is what Pipeline.Submit needs to carry one order.Intent
// through the full canonical M4 path. Unlike execution.PlanInput, it
// never carries an already-resolved Quantity: Pipeline itself owns
// deriving one (via Sizer, for an unsized order.IntentEnter) rather
// than accepting one as an external input that could disagree with
// what Sizer would have produced (#185 design discussion).
type Input struct {
	// Intent is what a strategy or operator wants to accomplish.
	Intent order.Intent

	// Listing is the concrete, venue-specific instrument this intent
	// targets. See execution.PlanInput.Listing's own doc comment for
	// the Instrument/Provider consistency this must satisfy.
	Listing instrument.Listing

	// Account is the current, authoritative account state this intent
	// is sized, planned, and risk-evaluated against.
	Account account.Snapshot

	// RiskFraction is the fraction of Account.Equity() Sizer may risk
	// when sizing an unsized order.IntentEnter (risk.SizeInput.
	// RiskFraction). Required, and must be positive, exactly when
	// Intent.Kind is order.IntentEnter; ignored otherwise.
	RiskFraction num.Rate

	// AdverseDistance is the adverse price-distance assumption this
	// intent is sized/admitted against: it is both Sizer's own
	// StopDistance for an unsized order.IntentEnter, and the value
	// threaded into risk.Input.AdverseDistance for any Rule that needs
	// one (for example PerTradeLossRule, #182). Required, and must be
	// positive, exactly when Intent.Kind is order.IntentEnter; optional
	// otherwise — a Rule that needs it regardless of Kind and finds it
	// nil returns its own classifiable ErrInsufficientRuleInput.
	AdverseDistance *num.Price

	// ReferencePrice is the valuation price threaded into
	// risk.Input.ReferencePrice, for a value-based Rule (for example
	// MaxInstrumentExposureRule, #183) that needs to price a resulting
	// position's notional exposure. Optional at this Input level for
	// the same reason risk.Input.ReferencePrice itself is: most rules
	// don't need one, and a rule that does and finds it nil returns its
	// own classifiable ErrInsufficientRuleInput.
	ReferencePrice *num.Price
}

// Result is what Pipeline.Submit produces for one Input. Proposal is
// populated once planning succeeds, even if risk later rejects it or
// the broker submission fails. Decision is populated once risk
// evaluation completes, even on rejection. Order is populated only
// after a successful broker submission — never on rejection, and never
// on a planning, sizing, or risk-evaluation failure.
type Result struct {
	Proposal order.Proposal
	Decision risk.Decision
	Order    order.Order
}

// Pipeline composes risk.Sizer, execution.Planner, risk.Engine, and
// broker.Broker into the canonical M4 orchestration path (#185,
// M4-10). See the package doc comment for the full flow and why this
// composition cannot live in execution or risk themselves.
type Pipeline struct {
	deps Deps
}

// NewPipeline returns a Pipeline backed by deps. Every field of deps
// must be set; see Deps.
func NewPipeline(deps Deps) (*Pipeline, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &Pipeline{deps: deps}, nil
}

// Submit carries in's Intent through the full canonical path: sizing
// (for an unsized order.IntentEnter), execution planning, risk
// admission, and — only on approval — broker submission via
// deps.Broker.OpenAccount(in.Account.AccountID()).Submit.
//
// Submit must be deterministic across independent Pipeline instances
// the same way execution.Planner.Plan and risk.Sizer.Size already are:
// given identical *initial* Deps state and an identical Input, two
// separately constructed Pipelines must produce identical Result
// values wherever the underlying Sizer/Planner/Engine/Broker
// implementations are themselves deterministic — Submit itself
// consults no clock, global randomness, or state beyond Input and
// Deps.
//
// A sizing, planning, or risk-evaluation error propagates wrapped but
// unwrapped-checkable (errors.Is) against its own package's
// classifiable sentinel — it is never collapsed into a generic
// pipeline error. A risk rejection is reported as ErrRejected, with
// Result.Proposal and Result.Decision populated so a caller can
// inspect exactly why; the broker is never called in that case.
func (p *Pipeline) Submit(ctx context.Context, in Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	validIntent, err := order.NewIntent(in.Intent)
	if err != nil {
		return Result{}, fmt.Errorf("%w: intent: %v", ErrInvalidInput, err)
	}
	if !strings.EqualFold(p.deps.Broker.Name(), in.Account.Broker()) {
		return Result{}, fmt.Errorf("%w: broker %q does not match account broker %q (Pipeline must never submit against a different broker than the account was sized/planned/risk-evaluated for)",
			ErrInvalidInput, p.deps.Broker.Name(), in.Account.Broker())
	}

	var qty *num.Quantity
	if validIntent.Kind == order.IntentEnter {
		if in.AdverseDistance == nil || in.AdverseDistance.IsZero() {
			return Result{}, fmt.Errorf("%w: adverse distance must be positive for %v", ErrInvalidInput, validIntent.Kind)
		}
		sized, err := p.deps.Sizer.Size(ctx, risk.SizeInput{
			Account:      in.Account,
			Listing:      in.Listing,
			RiskFraction: in.RiskFraction,
			StopDistance: *in.AdverseDistance,
		})
		if err != nil {
			return Result{}, fmt.Errorf("pipeline: sizing intent: %w", err)
		}
		qty = &sized
	}

	planResult, err := p.deps.Planner.Plan(ctx, execution.PlanInput{
		Intent:   in.Intent,
		Listing:  in.Listing,
		Account:  in.Account,
		Quantity: qty,
	})
	if err != nil {
		return Result{}, fmt.Errorf("pipeline: planning intent: %w", err)
	}

	decision, err := p.deps.Engine.Evaluate(ctx, risk.Input{
		Proposal:        planResult.Proposal,
		Account:         in.Account,
		AdverseDistance: in.AdverseDistance,
		ReferencePrice:  in.ReferencePrice,
	})
	if err != nil {
		return Result{Proposal: planResult.Proposal}, fmt.Errorf("pipeline: evaluating risk: %w", err)
	}
	if !decision.Allowed {
		return Result{Proposal: planResult.Proposal, Decision: decision}, ErrRejected
	}

	orderID, err := id.GenerateOrderID(p.deps.IDs)
	if err != nil {
		return Result{Proposal: planResult.Proposal, Decision: decision}, fmt.Errorf("pipeline: generating order id: %w", err)
	}
	req, err := order.NewRequest(planResult.Proposal, orderID)
	if err != nil {
		return Result{Proposal: planResult.Proposal, Decision: decision}, fmt.Errorf("pipeline: building request: %w", err)
	}

	acc, err := p.deps.Broker.OpenAccount(ctx, in.Account.AccountID())
	if err != nil {
		return Result{Proposal: planResult.Proposal, Decision: decision}, fmt.Errorf("pipeline: opening broker account: %w", err)
	}
	o, err := acc.Submit(ctx, req)
	if err != nil {
		return Result{Proposal: planResult.Proposal, Decision: decision}, fmt.Errorf("pipeline: submitting order: %w", err)
	}

	return Result{Proposal: planResult.Proposal, Decision: decision, Order: o}, nil
}
