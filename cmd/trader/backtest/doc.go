// Package backtest is the "trader backtest" command group (issue
// #222, M5-14): a thin CLI transport over service/backtest's own
// application service (ADR-022). Every leaf command parses its own
// flags into a service/backtest.RunRequest (or reads back a
// previously persisted report.BacktestReport), delegates the complete
// use case to service/backtest.Service, and renders the result via
// the report package's own Org/text/JSON renderers (issue #220) — this
// package contains no strategy, execution, risk, or metric business
// logic of its own, and never calls backtest.NewRunner/NewScheduler/
// NewReplay directly (boundary_test.go enforces this mechanically).
//
// # The provisional demo strategy
//
// "trader backtest run" accepts exactly one strategy today: an
// unexported demoStrategy (demo_strategy.go) that enters long once, on
// the requested instrument's own first bar, and never trades again.
// It exists solely so this command is genuinely executable end to end
// — it is not a real trading strategy, not the beginning of a strategy
// library, and not a registry. service/backtest.RunRequest.Strategy
// remains the real application contract; strategy discovery/naming is
// an explicitly deferred, later transport/composition concern (see
// ADR-039's own note and this issue's plan/review comments).
//
// # Persisted run snapshots, not journal replay
//
// "run" computes a report.BacktestReport exactly once (via
// report.NewBacktestReport) and persists that same projection as a
// small, schema-versioned JSON artifact under --output-dir
// (store.go). "show <run-id>" reads that artifact back and renders it
// — zero backtest orchestration, zero metric recomputation. The
// durable journal (adapters/journal/jsonl) remains a separate,
// lower-level audit trail this command group does not read from: it
// does not record the equity curve or backtest.Metrics in a
// reconstructable shape, so replaying it to answer "show" would mean
// re-deriving Metrics here — exactly the "second backtest
// orchestrator" this issue's own acceptance criteria says to avoid.
package backtest
