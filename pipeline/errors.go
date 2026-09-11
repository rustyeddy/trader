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

	// ErrBracketRequiresSubmit reports that Evaluate was called with an
	// order.IntentEnterWithStop (issue #351, ADR-059). A bracket
	// cannot be evaluated read-only: whether its second (stop) leg is
	// even viable depends on whether the first (entry) leg's own
	// broker submission actually filled, which is not knowable without
	// a real broker call. Use Submit instead.
	ErrBracketRequiresSubmit = errors.New("pipeline: order.IntentEnterWithStop cannot be evaluated read-only; call Submit")

	// ErrBracketEntryNotSynchronouslyFilled reports that an
	// order.IntentEnterWithStop's own entry leg was accepted by the
	// broker but did not fill synchronously, inside the same Submit
	// call (ADR-059) — so the stop leg was never attempted, and the
	// resulting position is open with no protective stop at all.
	//
	// This is expected, not a bug, against any broker that reports
	// fills asynchronously through its own event stream rather than
	// synchronously from Submit's own return value — every real
	// broker adapter today (adapters/broker/sim is the sole exception:
	// it fills a Market order synchronously, inside Submit itself).
	// ADR-059 records this as this decision's own deliberate scope: a
	// live/async-broker-safe bracket mechanism is a separate, tracked
	// follow-up (issue #366), not silently assumed solved by this
	// error existing. A caller that receives this error has a real,
	// unprotected open position and must decide how to protect it
	// (for example an immediate IntentAdjustStop retry against the
	// now-current account state, or an operator alert) — Pipeline
	// itself makes no such decision on the caller's behalf.
	ErrBracketEntryNotSynchronouslyFilled = errors.New("pipeline: bracket entry did not fill synchronously; stop leg not submitted")
)
