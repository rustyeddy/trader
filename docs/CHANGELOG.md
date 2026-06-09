# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.1] - 2026-06-04

### Added

- **Bot manager API** — `POST/GET/DELETE /api/v1/bots` lets the `trader serve`
  daemon start, inspect, and stop live strategy bots over HTTP. Bots run as
  goroutines inside the process with independent cancel contexts and status
  tracking (`running` / `stopped` / `error`).
- **Strategy factory in service layer** — `service.BuildLiveStrategy` centralises
  strategy construction so the REST API and CLI share identical logic. The CLI
  `buildStrategy` is now a thin shim delegating to the service.
- **`stress` strategy** — unconditional candle-based strategy that opens every N
  bars with no indicator warmup. Designed for API plumbing tests that must fire
  at any time. Stop distance stored as basis points (`StopBps int`); YAML accepts
  human-readable percentages (`stop_pct: 0.2` = 0.2% = 20 bps) with the float
  conversion isolated to the `build()` boundary.
- **Version stamped at build time** — `make build` now injects the current git tag
  via `-ldflags` so `trader version` returns the real tag (e.g. `v0.2.1`) instead
  of `dev`. Uses `git describe --tags --always --dirty`.
- **Donchian v2–v6 strategy variants** — progressive filter stack: v2 adds
  close-strength and confirm-bars; v3 adds D1 choppiness gate; v4 adds ADX gate;
  v5 adds news-day filter; v6 adds weekly ATR volatility gate.
- **Regime filters** — `AllowSide` interface, `weekly-ema`, `atr-percentile`,
  `session` (UTC hour window), `adx-d1` (H1→daily aggregation), and `composite`
  (AND combinator). Registered via `GetRegimeFilter` factory.
- **Bollinger Bands indicator + `bb-fade` mean-reversion strategy**.
- **Cross-pair instrument support** — cross-rate P/L conversion via approximate
  cross rates; all major G10 crosses now supported in the portfolio runner.
- **Portfolio live trading pipeline** — `trader live portfolio` runs multiple
  strategies across instruments concurrently against a single OANDA account.
- **Strategy replay API** — `POST /api/v1/replay` runs a strategy against stored
  candles and returns bars + signals. UI replay page renders signal markers and
  stop-trail overlay on the candlestick chart.
- **Structured live signal logging** — strategy signals emitted as structured
  `slog` events with reason, side, stop, and instrument fields.
- **Chandelier trailing stop in `CandleStrategyAdapter`** — exit strategy
  now primes from local candle store on warmup so stops are ready on first tick.
- **`data stats` command** — `Analyzer` interface with four built-in analyzers;
  `--units` flag shows USD notional alongside pip measurements.
- **`data pip-value` command** — USD pip values for all major pairs.
- **`data order-prices` command** — live bid/ask from OANDA for all major pairs.
- **`data position` command** — lot size ↔ USD notional calculator with optional
  live price fallback (`--price` or fetched from OANDA).
- **`gen-newsdays` tool** — generates news-day CSV calendars from a
  Dukascopy-format news event file for use with the v5 news filter.
- **Smoke test infrastructure** — `make smoke`, `make smoke-live`, `make smoke-live-dry`
  targets; `scripts/smoke.sh`; `testdata/configs/smoke-test.yml`.
- **Strategy sweep test** — all strategies × all instruments × H1 and D1.
- Global config search path with two-level layering (project + user).
- `--log-level`, `--log-format`, `--log-file` promoted to global persistent flags.
- `--log-file` defaults to `./trader.log` for all commands.
- Tick counts seeded from OANDA open-time on live runner restart.
- OANDA polling skipped automatically when forex market is closed.

### Fixed

- All dollar-value CLI output formatted to 2 decimal places.
- Replay `422` on daily timeframe: `"D"` accepted as alias for `D1`.
- `emitOpen` in Donchian no longer panics when `BacktestRequest` is nil.
- Cross-currency position sizing bug in `PlaceMarketOrder`.
- Default `--raw-dir` corrected: `/srv/trading/data/raw` (was `/srv/trading/raw`).
- Log output suppressed to stdout when a log file is configured.

## [v0.2.0] - 2026-05-27

### Added

- **Backtest: `run` and `regress` subcommands** — `backtest run` executes
  configs and writes timestamped JSON + org reports; `backtest regress`
  compares every metric against committed baselines in
  `testdata/backtests/reports/`, with `--update` to refresh baselines.
- **Hash-based report filenames** — reports are named
  `<name>-<hash8>.json` (8-char SHA256 of run parameters). Re-running the
  same config overwrites the same file; changing any param writes a new file
  alongside the old one.
- **Self-describing reports** — `BacktestReportSummary` now embeds
  `ConfigHash`, `GeneratedAt`, and a full `RunConfig` snapshot so every
  report JSON records exactly what produced it.
- **Backtest candle chart UI** — the backtests detail page now renders an
  interactive OHLC candlestick chart (lightweight-charts v5) with entry/exit
  trade markers. New REST endpoint `GET /api/v1/backtests/{name}/candles`
  serves the candle data.
- **MCP `download_candles` tool** — the MCP server (`trader mcp serve`)
  exposes a new write-gated tool to download OANDA candles for any
  instrument/timeframe/date-range directly into the local candle store.
  Requires `--enable-write`.
- **Donchian single-position guard** — `donchian-breakout` no longer stacks
  multiple entries in the same direction; a second breakout bar while already
  long/short is silently skipped. Reversal (close opposite, open new
  direction) is preserved.
- **`--reports-dir` flag** on `trader serve` to override the default backtest
  reports directory.
- **`ParseTimeRange`** exported helper in `types_time.go`.

### Fixed

- Config batch loader now recognises `*.yml`, `*.yaml`, and `*.json` glob
  patterns (previously only `*.yml` was matched).
- Default data directory corrected: `/srv/trading/data/candles` (was
  `/srv/trading/data`).
- Default backtest report directory corrected: `/srv/trading/backtests/reports`
  (was `/trading/backtests/results`).
- Chart snapback on browser zoom — fixed by absolute-positioning the
  lightweight-charts container and disabling `axisDoubleClickReset` on both
  axes (default is `true`, which resets bar spacing to 6 px/bar and
  compresses a year of H1 data to an unreadable sliver).
- `emitOpen` in Donchian no longer panics when `run.BacktestRequest` is nil
  (common in unit tests that only initialise `BacktestRun`).

### Changed

- `data_manager.go` / `trader.go`: default candle source changed from
  `SourceCandles` to `SourceOanda`.
- Report filenames use a param hash instead of a timestamp; the REST
  `loadSummary` helper always uses the filename stem as the report name.

## [v0.1.0] - 2025 (initial)

Initial tagged release. Core backtesting engine, Dukascopy + OANDA data
pipeline, REST API, SSE streams, SvelteKit UI, MCP server, and live OANDA
trading via the pulse strategy.
