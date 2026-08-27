package pipeline

import "errors"

var (
	// ErrInvalidDeps reports that Deps passed to NewPipeline is missing
	// a required dependency.
	ErrInvalidDeps = errors.New("pipeline: invalid deps")

	// ErrInvalidInput reports an Input that fails validation before
	// sizing or planning is attempted: an invalid Intent, an
	// unconstructed Listing/Account, or an order.IntentEnter with no
	// AdverseDistance for Sizer to size against. This is distinct from
	// a sizing, planning, or risk-evaluation failure — those propagate
	// as their own package's classifiable errors (ErrInvalidSizeInput,
	// ErrUnsupportedIntentKind, ErrInvalidInput from risk, and so on),
	// wrapped but never collapsed into this sentinel, per this issue's
	// own "explicit propagation of planning/risk failures" acceptance
	// criterion.
	ErrInvalidInput = errors.New("pipeline: invalid input")

	// ErrRejected reports that risk.Engine.Evaluate returned a Decision
	// with Allowed == false. Result.Proposal and Result.Decision are
	// still populated on this error so a caller can inspect exactly
	// which Rule(s) rejected the proposal; Result.Order is never
	// populated, since the broker is never called for a rejected
	// proposal.
	ErrRejected = errors.New("pipeline: risk rejected the proposal")
)
