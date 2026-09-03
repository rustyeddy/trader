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
// # Two strategy paths, no registry
//
// "trader backtest run" selects between exactly two strategies by
// whether --config is given — there is no strategy registry or
// name-based lookup (ADR-039's own note on deferring strategy
// discovery still applies beyond these two):
//
//   - Without --config: an unexported demoStrategy (demo_strategy.go)
//     that enters long once per requested instrument, on that
//     instrument's own first bar, and never trades that instrument
//     again. --symbol may be repeated (issue #224, M5-16) to run a
//     multi-instrument portfolio backtest with it — one Scheduler and
//     one shared account/pipeline still replay every requested
//     instrument, never a per-symbol engine. demoStrategy exists
//     solely so this command is genuinely executable end to end
//     without any real strategy configured; it is not a real trading
//     strategy.
//   - With --config (issue #252, EMA-07): the real strategy/emacross
//     EMA crossover strategy, configured from the YAML file's own
//     strategy.fast_period/slow_period (issue #247), for exactly one
//     instrument. Its FillPriceSource (nextBarOpenPriceSource,
//     service.go) is a general per-bar-lookup implementation, unlike
//     demoStrategy's precomputed single-fill price, because a
//     crossover strategy enters, exits, and re-enters at run-dependent
//     bars.
//
// service/backtest.RunRequest.Strategy remains the real application
// contract either way; this command constructs a concrete value for
// it, never a second orchestration path.
//
// # Persisted run snapshots, not journal replay
//
// "run" computes a report.BacktestReport exactly once (via
// report.NewBacktestReport) and persists that same projection as a
// small, schema-versioned JSON artifact under --output-dir
// (store.go). "show <run-id>" reads that artifact back and renders it
// — zero backtest orchestration, zero metric recomputation. An
// optional durable journal (adapters/journal/jsonl, --journal) may
// additionally be written during "run" as a lower-level audit trail,
// but this command group never reads from it: it does not record the
// equity curve or backtest.Metrics in a reconstructable shape, so
// replaying it to answer "show" would mean re-deriving Metrics here —
// exactly the "second backtest orchestrator" this issue's own
// acceptance criteria says to avoid. --journal is off by default (a
// nil Environment.Journal is accepted and treated as journal.Discard
// by backtest.Runner), so ordinary runs pay no cost for it.
//
// # Persistent canonical data store by default
//
// --data-store-root (issue #268) defaults to /srv/trading/data/
// canonical — a real, opinionated local path, not a research-neutral
// placeholder — so canonical market data built from --data-raw-root
// survives across invocations instead of being rebuilt from the raw
// archive into a fresh temporary directory every run. This default is
// resolved through the same runConfig/backtestSection config-loading
// path (experimentconfig.go) as every other backtest setting, so an
// explicit --data-store-root flag, a --config file value, or a
// TRADER_BACKTEST_DATA_STORE_ROOT environment variable all still take
// precedence over it in that order. An explicit empty value at any of
// those layers opts back into runBacktest's original fresh-temporary-
// directory behavior — what every test in this package that must not
// share store state across runs relies on.
package backtest
