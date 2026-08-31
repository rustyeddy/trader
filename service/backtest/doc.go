// Package backtest is the application/service layer for Trader's M5
// backtest use case (ADR-022, issue #221, M5-13).
//
// Service wraps a *marketdata.Manager, an instrument.Resolver, and an
// injected EnvironmentFactory, exposing the complete "run a backtest
// and get back its result" use case as one transport-neutral
// operation: Run. A caller (a future CLI or REST adapter) constructs a
// Service over whichever concrete composition the caller's own
// composition root chose; Service itself never imports or names a
// concrete broker adapter, matching every other service subpackage's
// "no concrete adapters" rule.
//
// # Why an injected EnvironmentFactory
//
// backtest.Runner is single-use: RunnerParams bundles a freshly
// constructed Clock, IDs, Account, and Pipeline for exactly one run,
// and Runner's own doc comment says building that fresh, mutually
// consistent bundle "belongs to a factory one tier up (service/backtest
// or cmd/trader/backtest), not to Runner itself." Every other service
// subpackage (service/execution, service/broker) instead reuses one
// long-lived broker.Broker/*pipeline.Pipeline across many calls,
// injected by its own caller — that pattern does not fit here, because
// a backtest run needs a brand-new broker/account/pipeline every
// single call (simulated state must never leak between runs).
//
// Service therefore takes an EnvironmentFactory at construction:
// something that builds one fresh, self-consistent Environment
// (Clock, IDs, Account, Pipeline, the descriptive ComponentInfo trio,
// and a run-scoped Journal) per Run call. The concrete choice of
// simulated broker, risk engine, and execution planner lives entirely
// behind that factory — Service's own dependency graph stays free of
// adapters/broker/sim, matching every other service package's
// boundary.
//
// # Scope
//
// Service never formats a response and never depends on a transport
// framework — see the service package's own doc comment for the full
// set of rules every service subpackage follows. RunResponse mirrors
// backtest.Result's meaningful fields (matching
// service/execution.SubmitResponse's own "mirror the domain result"
// convention) rather than returning backtest.Result directly, and
// carries no reporting or serialization concerns — report.
// NewBacktestReport (issue #220) remains the presentation boundary
// over whichever Result-shaped value a caller obtains.
package backtest
