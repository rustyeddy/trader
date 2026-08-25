package execution

import "errors"

var (
	// ErrInvalidDeps reports that Deps passed to NewPlanner is missing a
	// required dependency.
	ErrInvalidDeps = errors.New("execution: invalid deps")

	// ErrInvalidPlanInput reports a PlanInput that fails validation:
	// an invalid Intent, a Listing that does not identify Intent's own
	// Instrument, an unconstructed Account, or a Quantity present/absent
	// in violation of Intent.Kind's own requirement.
	ErrInvalidPlanInput = errors.New("execution: invalid plan input")

	// ErrUnsupportedIntentKind reports an Intent.Kind this Planner does
	// not yet know how to plan. IntentAdjustStop is the current example
	// (issue #179's own design discussion): modifying an existing
	// protective stop is a pre-risk replacement-proposal concept this
	// package does not define yet, not a new-order Proposal.
	ErrUnsupportedIntentKind = errors.New("execution: intent kind not supported by this planner")

	// ErrNoPositionToExit reports an IntentExit against an instrument
	// the account has no open position in — there is nothing to close.
	ErrNoPositionToExit = errors.New("execution: no open position to exit")

	// ErrAlreadyAtTarget reports an IntentTargetExposure whose desired
	// Side and Quantity already match the account's current position
	// exactly — there is no delta to propose.
	ErrAlreadyAtTarget = errors.New("execution: account is already at the target exposure")
)
