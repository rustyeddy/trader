// Package eq09 is issue #302 (EQ-09)'s end-to-end SPY equity
// paper-trading smoke test — the capstone of the Equities Phase 1
// milestone. It demonstrates that Trader can resolve a US equity
// instrument, read its canonical market data through
// marketdata.Manager, and carry a real trading decision through the
// normal execution/risk/pipeline path to a real Alpaca paper account,
// using shared architecture rather than a bespoke direct-to-broker
// path — the same proof EQ-07 (#309) already gave for backtesting.
//
// # Scope: a one-shot smoke procedure, not a live session
//
// This package builds no live-session, guard, reconciliation, or
// kill-switch machinery (ADR-014's "paper is the safe default,"
// startup reconciliation, stale-data detection, and so on). Those are
// the future =live= package's own scope (a later milestone); nothing
// under that name exists in this repository yet. EQ-09 runs once,
// observes its own result, and exits — it is not a persistent trading
// process.
//
// # "Normal strategy/decision/order pipeline," scoped
//
// No strategy.Strategy implementation or bar-replay runner is
// involved: strategy.Strategy is driven by backtest.Runner today, and
// building a live runner is exactly the future =live= package's job,
// not this smoke test's. The actual normal path from a trading
// decision to a broker order is order.Intent -> pipeline.Pipeline
// (Sizer -> Planner -> Risk -> Broker) — the same path
// service/execution/vertical_slice_test.go already exercises against
// the simulator. This package exercises the identical path against a
// real broker.Broker (adapters/broker/alpaca) instead of the
// simulator, composed the same way a real composition root would: via
// each lower package's own public constructors
// (execution.NewPlanner, risk.NewEngine, pipeline.NewPipeline,
// service/execution.New), never reaching into any package's
// unexported internals.
//
// # Running it
//
// This package's only test is opt-in, gated behind the "alpacasmoke"
// build tag already established by the Alpaca market-data provider
// (issue #297) and the Alpaca broker adapter (issue #301) — reused
// here rather than inventing a fourth opt-in mechanism for the same
// "real Alpaca credentials, never committed" purpose. See
// smoke_test.go's own doc comment for exact prerequisites (an Alpaca
// paper account, an API key/secret pair, and a real NYSE regular
// trading session) and docs/research/eq-09-spy-paper-smoke.org for the
// full documented procedure and its recorded results.
package eq09
