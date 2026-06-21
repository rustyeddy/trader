# Trader Roadmap

This document contains future work only. It is not a release commitment or a
substitute for GitHub issues. Current behavior is documented in
[architecture.org](architecture.org), [Configuration.md](Configuration.md),
[Services.md](Services.md), and [oanda.md](oanda.md).

Completed work is removed rather than retained as a historical checklist; Git
history and the changelog provide that record.

## Priority: correctness and safety

### Automatic margin-call liquidation

Implement deterministic forced liquidation after account revaluation makes
`FreeMargin < 0`. Positions must close through the normal broker/account event
path until margin is restored or no positions remain.

Tracking: [issue #147](https://github.com/rustyeddy/trader/issues/147).

### Eliminate internal floating-point arithmetic

Convert prices, indicators, strategies, account calculations, live adapters,
and sizing logic to scaled domain types immediately after external input.
Retain floating point only at configuration, broker-wire, and presentation
boundaries, with a static check preventing regression.

Tracking: [issue #148](https://github.com/rustyeddy/trader/issues/148).

### Stable request model

Unify strategy, alert, webhook, CLI, REST, and broker order inputs around one
validated request model without leaking transport-specific types into the
domain.

Tracking: [issue #83](https://github.com/rustyeddy/trader/issues/83).

### Netting-aware live execution

OANDA accounts can net opposite-side exposure on one instrument. Add explicit
policy for adopting, closing, or rejecting opposite-side trades before a new
order. Ensure strategy state and broker state cannot silently diverge.

### Startup reconciliation and orphan positions

Live runners already estimate position age from OANDA `openTime` after a
restart. Remaining work is to reconcile every broker position with a managed
bot/strategy, define adoption policy for unknown positions, and surface
unmanaged exposure prominently.

### Order retry policy

FOK cancellation currently returns an error and the runner skips that order.
Add an opt-in bounded retry policy with jitter, idempotency safeguards, and
clear logging. Never retry closes or opens blindly.

## Backtesting and research

### Walk-forward testing

Add in-sample optimisation and out-of-sample validation windows with a combined
report. Preserve deterministic inputs and make window boundaries explicit.

### Portfolio backtests

Run several instruments against shared capital and margin state. Define candle
clock alignment, missing-bar handling, correlation reporting, and deterministic
order when multiple instruments act at the same timestamp.

### Parameter optimisation

Add grid and bounded random search over registered strategy parameters. Record
the complete search space, seed, data hash, ranking metric, and out-of-sample
results so rankings are reproducible.

### Execution-cost model

Fixed adverse slippage and candle spread filtering are implemented. Remaining
work includes commission, financing/rollover, configurable stochastic
slippage, and sensitivity reports. Any random model must use an explicit seed.

### Equity and exposure time series

Persist per-bar equity, drawdown, margin, and exposure snapshots in backtest
results so UI charts and portfolio analysis do not reconstruct them from
closed trades.

## Strategy engine

### External strategies

Allow strategies to run without recompiling the main binary. Preferred
direction is a versioned subprocess protocol over newline-delimited JSON:

- language-independent and process-isolated;
- explicit startup/health/shutdown lifecycle;
- bounded message sizes and deadlines;
- deterministic replay support;
- no access to broker credentials by default.

Starlark remains a possible deterministic scripting option. Go's `plugin`
package remains unsuitable because of platform and build-identity constraints.

### Risk-sizing modes

Support explicit, testable sizing policies rather than embedding sizing rules
inside strategies. Candidate policies include fixed fractional,
anti-martingale, volatility targeting, and capped fractional Kelly.

Martingale sizing should not be a production default; if implemented for
research, it must carry hard exposure/drawdown caps and prominent warnings.

### Strategy metadata

Expose machine-readable parameter schemas, defaults, warmup needs, compatible
timeframes, and descriptions from each registered strategy. Reuse that
metadata in CLI help, validation, MCP, and the UI.

## UI

### Backtest browser and result comparison

Complete searchable result browsing, side-by-side diffs, equity/drawdown
charts, and links to candle/signal context.

Tracking: [issue #114](https://github.com/rustyeddy/trader/issues/114).

### Strategy launcher

Run backtests and parameter sweeps from the UI with explicit resource limits
and progress/error reporting.

Tracking: [issue #115](https://github.com/rustyeddy/trader/issues/115).

### Live chart and overlays

Stream current candles and position overlays, then add ATR, ADX, and
Choppiness panels.

Tracking:
[issue #117](https://github.com/rustyeddy/trader/issues/117) and
[issue #118](https://github.com/rustyeddy/trader/issues/118).

### Live bot controls

REST and service bot lifecycle APIs exist. Remaining UI work is to list,
start, stop, and inspect bots safely, with clear practice/live status and
confirmation for side effects.

### Browser tests

Add Playwright coverage for dashboard, trades, backtests, charts, replay, and
bot controls against an isolated test server. Live broker calls must be mocked.

## API and MCP

The MCP server already exposes account/trade data, prices, candle CSV/stats,
validation, pip/position calculations, backtests, bot reads, and gated write
tools.

Remaining high-value parity work:

- `replay`: expose `service.RunReplay` for iterative strategy analysis;
- `review`: parse uploaded/server-side review CSV through
  `service.ParseReviewCSV`;
- `list_backtests`: filtered saved-report discovery rather than only resource
  listing;
- `get_backtest_candles`: retrieve candle context for a saved result.

SSE streaming is intentionally an HTTP concern rather than an MCP tool.
Autonomous live-run loops should remain outside MCP unless a strong
authorization and lifecycle model is added.

## Operations and deployment

### Configuration cleanup

Unify configuration naming and precedence where command-specific `--config`
flags currently overlap global configuration. Add strict unknown-field
validation and remove the legacy private `appConfig`.

Tracking: [issue #120](https://github.com/rustyeddy/trader/issues/120).

### Logging fan-out

Add an in-memory bounded log stream with subscriber fan-out for the UI and
diagnostics, without allowing slow consumers to block trading loops.

Tracking: [issue #119](https://github.com/rustyeddy/trader/issues/119).

### Deployment automation

Docker, Compose, and systemd assets exist. Remaining work is repeatable
environment provisioning, secret delivery, upgrade/rollback, health checks,
and separate practice/live inventories. Ansible roles are one possible
implementation.

### Durable journal backend

CSV and JSONL journals are implemented; PostgreSQL is named but not
implemented. Select and implement a queryable durable backend only after
retention, migration, concurrency, backup, and operational requirements are
defined. Do not describe the removed SQLite backend as available.

### Data backup and recovery

Automate backups for raw and canonical candle data, journal records, configs,
and reports. Document restore verification rather than treating a successful
upload as a tested backup.
