// Package execution defines Trader's execution-planning contract
// (ADR-006, issue #179, M4-04): the seam that translates one
// order.Intent into a broker-neutral order.Proposal without approving
// risk or submitting anything to a broker.
//
// # Scope
//
// Planner.Plan is deliberately narrow: it consumes a fully-resolved
// PlanInput (Intent, the concrete Listing to target, current Account
// state, and — only for an unsized IntentEnter — an already-decided
// Quantity) and returns exactly one Proposal or a classifiable error.
// It never fetches its own account state, market data, clock, or
// listing resolution; those are supplied by a composition root.
//
// This package does not decide risk admission (that is risk's job,
// ADR-006) and does not size an IntentEnter itself: ADR-006 assigns
// sizing to risk, not execution, so Planner consumes an
// already-resolved Quantity through PlanInput rather than owning or
// calling a sizing policy of its own. See PlanInput's own doc comment.
//
// # Supported intent kinds
//
// planner, the v0 Planner this package provides, supports
// order.IntentEnter, order.IntentExit, and order.IntentTargetExposure.
// order.IntentAdjustStop is not supported yet and returns
// ErrUnsupportedIntentKind: modifying an existing protective stop is
// conceptually a pre-risk *replacement* proposal (something that would
// eventually become an order.ReplaceRequest against an existing order,
// after risk approval), not a new-order Proposal — a distinct
// vocabulary this package does not define yet, deferred until a real
// consumer needs it rather than speculated into this issue.
//
// # Dependency direction
//
// execution depends only on order, account, instrument, num, id, and
// clock — never on broker or risk (ADR-006's own "execution does not
// depend on strategy or risk" rule, package-boundaries.org). See
// boundary_test.go for the mechanical guard.
package execution
