// Package pipeline implements Trader's canonical M4 orchestration path
// (issue #185, M4-10): the seam that turns one order.Intent into a
// broker submission candidate only after sizing, execution planning,
// and risk admission all succeed.
//
//	Intent -> risk.Sizer (IntentEnter only) -> execution.Planner ->
//	order.Proposal -> risk.Engine -> approved order.Request ->
//	broker.Account.Submit
//
// Evaluate (issue #187's design discussion) is the read-only prefix of
// that same path: it runs every stage through building the approved
// order.Request, but never calls the broker. Submit is its thin
// mutating continuation — it calls Evaluate, then submits the exact
// Request Evaluate already built when risk approves — so there is
// exactly one implementation of the sizing/planning/risk/request-
// construction sequence, not two.
//
// # Why this package exists
//
// execution and risk are deliberately independent siblings (ADR-006):
// neither imports the other, and neither imports broker — see each
// package's own boundary_test.go. Something has to compose all three
// plus assign the resulting order.OrderID and call
// broker.Account.Submit, and that composition cannot live inside
// execution or risk without reversing one of those boundaries. This
// package is that composition, matching the architecture document's
// own "application orchestration" tier, one level above
// strategy/execution/risk/broker.
//
// # Scope
//
// Pipeline.Submit owns the full canonical path, including sizing: per
// the M4-10 design discussion on #185, requiring every caller
// (backtest, live, a future service/CLI layer) to reproduce
// sizing-then-planning-then-risk-then-submit itself would defeat the
// purpose of a shared orchestration seam. A caller supplies an Intent,
// the Listing/Account it applies to, and the risk/sizing assumptions
// (RiskFraction, AdverseDistance, ReferencePrice) a concrete Sizer or
// Rule might need; Pipeline resolves everything after that.
//
// Risk rejection never reaches the broker: Evaluate (and therefore
// Submit, which calls it) returns before generating an order.OrderID
// or building an order.Request when risk.Decision.Allowed is false,
// satisfying "risk rejection prevents broker mutation/events" by
// construction, not by a caller checking a flag afterward.
//
// This package does not implement a CLI, transport-neutral
// request/response DTOs, or service-layer error mapping — that is the
// ADR-022 application service issue #186 builds on top of Pipeline.
package pipeline
