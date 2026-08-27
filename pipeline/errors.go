package pipeline

import "errors"

var (
	// ErrInvalidDeps reports that Deps passed to NewPipeline is missing
	// a required dependency.
	ErrInvalidDeps = errors.New("pipeline: invalid deps")

	// ErrInvalidInput reports an Input that fails the structural checks
	// Pipeline itself performs before sizing or planning is attempted:
	// an invalid Intent (order.NewIntent's own validation), a
	// deps.Broker whose Name() does not case-insensitively match
	// in.Account.Broker() (Pipeline must never submit against a
	// different broker than the account was sized/planned/
	// risk-evaluated for), or an order.IntentEnter with no
	// AdverseDistance for Sizer to size against.
	//
	// Pipeline deliberately does not duplicate every structural check
	// its own Sizer/Planner/Engine dependencies already perform: an
	// unconstructed Listing/Account, for example, is instead reported
	// by the underlying risk.ErrInvalidSizeInput or
	// execution.ErrInvalidPlanInput once sizing or planning runs. Those
	// — along with any other sizing, planning, or risk-evaluation
	// failure — propagate as their own package's classifiable errors,
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
