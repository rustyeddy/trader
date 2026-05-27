# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
