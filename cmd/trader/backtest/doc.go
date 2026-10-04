// Package backtest is the "trader backtest" command group (issue
// #222, M5-14): a thin CLI transport (ADR-022). "run" parses its flags,
// --config file, and environment into a backtestcfg.RunConfig, opens the
// market data and optional --journal for this invocation, and runs the
// backtest through backtestcfg.Run — the shared composition trader-mcp
// also uses (issue #453, ADR-069) — then persists the report through
// service/backtest.RunStore and renders it with the report package's
// Org/text/JSON renderers (issue #220). "show" reads a persisted report
// back and renders it. This package contains no strategy, execution,
// risk, or metric business logic of its own, and never calls
// backtest.NewRunner/NewScheduler/NewReplay directly (boundary_test.go
// enforces this mechanically). See backtestcfg's package doc for how a
// strategy is selected.
//
// # Persisted run snapshots, not journal replay
//
// "run" computes a report.BacktestReport exactly once (via
// service/backtest.NewReport) and persists that same projection as a
// small, schema-versioned JSON artifact under the output directory
// (--output-dir, backtest.output_dir, or TRADER_BACKTEST_OUTPUT_DIR;
// default ./backtest-runs) through service/backtest.RunStore. "show <run-id>" reads that artifact back and renders it
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
// resolved through the same backtestcfg.RunConfig config-loading
// path (config.go) as every other backtest setting, so an
// explicit --data-store-root flag, a --config file value, or a
// TRADER_BACKTEST_DATA_STORE_ROOT environment variable all still take
// precedence over it in that order. An explicit empty value at any of
// those layers opts back into runBacktest's original fresh-temporary-
// directory behavior — what every test in this package that must not
// share store state across runs relies on.
package backtest
