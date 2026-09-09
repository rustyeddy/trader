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
	// not yet know how to plan.
	ErrUnsupportedIntentKind = errors.New("execution: intent kind not supported by this planner")

	// ErrNoPositionToExit reports an IntentExit against an instrument
	// the account has no open position in — there is nothing to close.
	ErrNoPositionToExit = errors.New("execution: no open position to exit")

	// ErrAlreadyAtTarget reports an IntentTargetExposure whose desired
	// Side and Quantity already match the account's current position
	// exactly — there is no delta to propose.
	ErrAlreadyAtTarget = errors.New("execution: account is already at the target exposure")

	// ErrNoPositionToProtect reports an IntentAdjustStop against an
	// instrument the account has no open position in — there is
	// nothing for a protective stop to protect.
	ErrNoPositionToProtect = errors.New("execution: no open position to protect with a stop")

	// ErrExistingStopOrder reports Plan called with IntentAdjustStop
	// against an instrument that already has a resting Stop order
	// (issue #336): initial stop placement is a new-order Proposal,
	// which Plan handles directly, but ratcheting an existing stop is
	// a replacement, which Plan deliberately does not — see
	// PlanReplace instead. A caller (pipeline.Pipeline) is expected to
	// check for a resting stop order before deciding which of Plan/
	// PlanReplace to call; this error exists so a caller that gets
	// that dispatch wrong fails loudly and classifiably rather than
	// silently planning a second, redundant stop order.
	ErrExistingStopOrder = errors.New("execution: instrument already has a resting stop order; use PlanReplace")

	// ErrNoRestingStopOrder reports PlanReplace called with
	// IntentAdjustStop against an instrument with no existing resting
	// Stop order to replace — see Plan instead for initial placement.
	ErrNoRestingStopOrder = errors.New("execution: no resting stop order to replace")
)
