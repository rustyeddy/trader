// Package execution is the application/service layer for Trader's M4
// execution/risk use case (ADR-022, issue #186, M4-11).
//
// Service wraps a broker.Broker (github.com/rustyeddy/trader/broker)
// and a *pipeline.Pipeline (github.com/rustyeddy/trader/pipeline,
// issue #185/M4-10) and exposes the complete sizing -> planning ->
// risk -> broker-submission use case as two transport-neutral
// operations: Evaluate (read-only: sizing through the approved
// order.Request, never mutating or submitting to the broker — it does
// still read a fresh account.Snapshot, see "Snapshot freshness, not
// atomicity" below) and Submit (Evaluate plus broker submission, issue
// #187's design discussion). A caller (the
// CLI today, a future REST/WebSocket/SSE adapter) constructs a Service
// over whichever broker.Broker and *pipeline.Pipeline the composition
// root chose; Service itself never imports or names a specific broker
// adapter.
//
// # Why two separately injected dependencies
//
// pipeline.Pipeline.Submit deliberately never fetches its own
// account.Snapshot — pipeline.Input.Account is caller-supplied,
// matching execution.PlanInput/risk.Input's own established "never
// fetch your own account state" convention (ADR-006). Something has to
// open the account and pull a fresh snapshot before delegating to
// Pipeline, and per this issue's own scope — "coordinate account
// snapshot retrieval, planning, risk evaluation, and approved broker
// submission" — that coordination is exactly this service's job, not
// Pipeline's. Service therefore holds both a broker.Broker (to obtain
// the authoritative state for the use case) and a *pipeline.Pipeline
// (for the already-settled sizing/planning/risk/submit sequence);
// the broker dependency is not duplicating Pipeline's own orchestration,
// it serves this distinct application-service responsibility.
//
// The normal composition root wires both from the same underlying
// broker.Broker instance the injected *pipeline.Pipeline was itself
// constructed with, but Service does not rely on that as its
// correctness mechanism: pipeline.Pipeline.Submit independently
// verifies its own injected broker.Broker.Name() matches
// Input.Account.Broker() before any broker mutation (PR #200's own
// boundary invariant), so a service accidentally wired with a
// mismatched broker/Pipeline pairing still fails safely — the mismatch
// is rejected before broker submission, not merely relied upon to
// never occur. See submit_test.go's own mismatched-wiring regression.
//
// # Snapshot freshness, not atomicity
//
// Evaluate/Submit's shared OpenAccount -> Snapshot -> Pipeline.
// Evaluate/Submit sequence gives sizing, planning, and risk evaluation
// a fresh authoritative snapshot for each call, but it is still a
// point-in-time read, never an atomic snapshot-and-submit transaction:
// live broker state can change between the Snapshot call and the
// eventual broker.Account.Submit call inside Pipeline.Submit.
// Reconciling against that possibility is explicitly out of M4's scope
// (live-session guards and reconciliation are Milestone 7 concerns).
//
// # Scope
//
// Service never reaches into an adapter's own internal package, never
// formats a response, and never depends on a transport framework — see
// the service package's own doc comment for the full set of rules
// every service subpackage follows. Response fields are Trader domain
// values (order.Proposal, risk.Decision, order.Order) rather than any
// CLI- or JSON-specific DTO shape — those decisions belong to whatever
// transport eventually renders this response.
//
// # Logging and risk rejection
//
// Every Service operation logs exactly one structured completion or
// failure record (log.go, ADR-023), scoped with the canonical
// logging.ComponentExecution attribute. A risk rejection
// (errors.Is(err, pipeline.ErrRejected)) is logged as an expected,
// structured admission decision — allowed=false plus the same request-
// identifying attributes — never at error/failure severity: it is a
// normal business outcome of evaluating risk, not an operational
// service failure. Every other failure (OpenAccount, Snapshot, sizing,
// planning, or broker submission) logs at error severity, matching
// service/broker's own convention. The returned error remains
// errors.Is-checkable against pipeline.ErrRejected either way — this
// only changes observability severity, not the error contract.
package execution
