# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build          # build bin/trader
make test           # run all tests
make vet            # go vet ./...
make cover          # coverage report
make cover-html     # HTML coverage report
make test-blackbox  # tests tagged `blackbox`
make install        # install to GOPATH/bin
make clean          # remove bin/ and coverage files

# Run a single test
go test -run TestName ./...

# Enable Dukascopy download tests (hits network)
TRADER_RUN_DUKASCOPY_TESTS=1 go test ./...
```

## Architecture

This is an FX (forex) backtesting and paper-trading engine. The core loop:

1. **Config** (YAML) defines runs: instrument, date range, capital, risk %, strategy
2. **DataManager** loads/caches OHLC candles (Dukascopy or OANDA as source)
3. **Backtest** iterates candles, calling `Strategy.Update()` each bar
4. **Strategy** returns an action (open/close), submitted to **Broker**
5. **Broker** fills immediately (market order simulation), emits `Event`
6. **Account** updates equity, margin, unrealized P/L on every price tick
7. **Journal** (CSV or SQLite) records closed trades and equity snapshots

CLI entry is `cmd/main.go` (Cobra). Subcommands: `backtest`, `data`, `replay`.

## Key Types

**Fixed-point numeric types** — all prices and money are scaled integers, never floats:
- `Price` (int32) — scaled by `PriceScale` (100,000)
- `Money` (int64) — scaled by `MoneyScale` (1,000,000)
- `Rate` (int64) — exchange rate scaled by `RateScale`
- `Units` — position size in micro-lots

**Core domain types:**
- `Account` — balance, equity, margin, open positions, closed trades
- `Position` — open trade (entry price, units, side, stop/take)
- `Trade` — closed position with realized P/L
- `Candle` — OHLC + spread for one time bar
- `Instrument` — FX pair metadata (base/quote currencies, margin rate, pip location)

**Engine types:**
- `Backtest` — wraps `BacktestRequest` + `BacktestRun` + `BacktestResult`
- `Trader` — coordinates DataManager + Broker + Account + Journal
- `Broker` — executes `OpenRequest`/`closeRequest`, emits events on a channel
- `Strategy` interface — `Update(ctx, candle, backtest) → StrategyPlan`
- `DataManager` — candle cache and loader; `CandleIterator` for traversal
- `Journal` interface — write-only; `CSVJournal` and `SQLiteJournal` implementations

## Accounting Invariants

These must hold after every operation:
- `Equity = Balance + UnrealizedPL`
- `FreeMargin = Equity − MarginUsed`
- BUY: open at ask, close at bid; SELL: open at bid, close at ask
- P/L calculated in quote currency, then converted to account currency via `QuoteToAccount()`
- Stop/take-profit evaluated on **every price update** (inclusive)
- Forced liquidation triggers when `FreeMargin < 0`; it must always leave `Equity ≥ MarginUsed`

## Configuration

Backtests are driven by YAML files. See `testdata/configs/` for examples. A config has:
- `defaults` — shared settings (capital, risk, data directory)
- `runs[]` — list of `RunConfig` (instrument, startDate, endDate, strategy name + params)

## Testing Conventions

**Every code change must ship with tests. No exceptions.**

- New functions, handlers, and types require unit tests with maximum coverage
- Modified functions must have their tests updated to cover the changed behaviour
- Run `make test` and `make cover` before committing; address gaps in coverage
- Use `testify` assertions (`require`, `assert`)
- Historical candle fixtures live in `testdata/candles/`
- Config fixtures live in `testdata/configs/`
- Synthetic candle generators exist for deterministic unit tests
- Tests are deterministic: same inputs always produce same outputs (no randomness)
- REST handlers: use `httptest.NewRecorder` + `httptest.NewRequest`; no real server needed
- Table-driven tests preferred for functions with multiple input/output cases
