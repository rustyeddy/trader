# Agent Instructions

This file is the canonical guidance for AI agents (Claude Code, GitHub Copilot, OpenAI Codex,
and others) working in this repository. All agent-specific config files symlink here.

## Build, test, and lint commands

```bash
make vet                 # go vet ./...
make lint                # staticcheck ./... (catch dead code, capitalized errors, etc.)
make test                # full Go test suite
make build               # build bin/trader (requires existing ui/dist assets)
make build-full          # rebuild the Svelte UI, then rebuild bin/trader
make cover               # write coverage.out
make cover-html          # write coverage.out and coverage.html
make blackbox            # run tests tagged blackbox
make sweep               # runs TestStrategySweep — broad smoke test across all registered strategies
make tidy                # run go mod tidy after dependency changes
make install             # install to GOPATH/bin
make clean               # remove bin/, coverage files, ui/dist, and ui/.svelte-kit

# Format changed Go files before testing
gofmt -w path/to/changed.go

# Run one test
go test ./... -run TestName

# Run one test in a specific package
go test ./service -run TestName

# Optional network-hitting Dukascopy tests
TRADER_RUN_DUKASCOPY_TESTS=1 go test ./...
```

If you touch the embedded UI, install deps in `ui/` first on fresh clones (`cd ui && npm ci`)
and then use `make build-full` or `cd ui && npm run build`. For UI-only checks,
`cd ui && npm run check` runs the Svelte/TypeScript checker.

The Go toolchain version is declared in `go.mod` (currently Go 1.24). UI dependency versions
are locked by `ui/package-lock.json`; use `npm ci`, not `npm install`, on clean checkouts.

## Architecture

This is an FX (forex) backtesting and live-trading engine.

**Core backtest loop:**
1. **Config** (YAML) defines runs: instrument, date range, capital, risk %, strategy
2. **DataManager** loads/caches OHLC candles (Dukascopy or OANDA as source)
3. **Backtest** iterates candles, calling `Strategy.Update()` each bar
4. **Strategy** returns an action (open/close), submitted to **Broker**
5. **Broker** fills immediately (market order simulation), emits `Event`
6. **Account** updates equity, margin, unrealized P/L on every price tick
7. **Journal** records closed trades and equity snapshots through the configured persistence format

**Package layout:**
- `cmd/main.go` — Cobra entrypoint; wires subcommands and blank-imports data provider and
  strategy packages so their `init()` registration runs before config-driven execution.
- `service/` — protocol-agnostic business layer shared by CLI commands, `api/rest`, and
  `api/mcp`. Keep trading, order, replay, journal, and bot logic here; presentation layers
  should stay thin.
- `api/rest` and `api/mcp` — thin transport layers; parse inputs, call `service` methods,
  map results/errors at the edge.
- `cmd/` — CLI handlers; same rule: no business logic, delegate to `service`.

**Backtest flow:** YAML config (`Config` / `RunConfig`) → `CompileBacktests()` resolves
defaults → `service.RunBacktest()` → `TraderBacktestExecutor` builds `Trader` + `Broker` +
`Account` → `Trader.Backtest()` iterates candles and drains broker events → summaries/reports
written by `cmd/backtest` or exposed through REST.

**Data pipeline:** `DataManager` scans store inventory, builds a wantlist, plans missing work,
downloads Dukascopy ticks, and builds canonical M1/H1/D1 candle files. `service/data.go` can
also import OANDA candles into the same store layout.

**Live trading:** `service.RunLiveStrategy()` fetches current OANDA prices, loads open trades,
asks the strategy for a `LivePlan`, executes closes first, then places a new market order if
requested. Portfolio mode runs one goroutine per instrument and wraps strategies with a shared
drawdown circuit breaker.

**Serve daemon:** `trader serve` creates a `service.Service`, starts the REST API, mounts
embedded UI assets from `ui/dist`, and runs the live journal stream with reconnect backoff.

## What NOT To Do

**No floats in internal calculations — ever.**

This is the single most important rule in this codebase. All accounting, pricing, stop levels,
take-profit levels, indicator calculations, position sizing, P/L, margin, equity, and spread
arithmetic must use the fixed-point domain types (`Price`, `Money`, `Rate`, `Units`).

Floats are **only** permitted at the following boundaries:
- Parsing values from YAML config or CLI flags
- Parsing API responses from OANDA or other brokers
- Formatting human-readable output (reports, logs, CLI display)
- Outbound API calls to a broker that require a float wire format

If you are about to write `float64` anywhere that is not one of the four cases above, stop.
Convert to the appropriate fixed-point type instead. Introducing floats inside the engine
causes silent precision loss that compounds across thousands of bars and corrupts backtest
results.

Existing internal floating-point violations are migration debt tracked in
[GitHub issue #148](https://github.com/rustyeddy/trader/issues/148). Do not copy them or add new
ones.

## Key Types

**Fixed-point numeric types** — all prices and money are scaled integers, never floats:
- `Price` (int32) — scaled by `PriceScale` (100,000)
- `Money` (int64) — scaled by `MoneyScale` (1,000,000)
- `Rate` (int64) — exchange rate scaled by `RateScale`
- `Units` (int64) — fixed-point position size or multiplier, scaled by `UnitsScale` (1,000,000)
- `Pips` (int32) — stop/take-profit distances stored in deci-pips (`1 == 0.1 pip`)
- `Scale6` (int32) — price-scale type; `PriceScale` is 100,000 (five fractional digits)

**Core domain types:**
- `Account` — balance, equity, margin, open positions, closed trades
- `Position` — open trade (entry price, units, side, stop/take)
- `Trade` — closed position with realized P/L
- `Candle` — OHLC + spread for one time bar
- `Instrument` — FX pair metadata (base/quote currencies, margin rate, pip location)

**Engine types:**
- `Backtest` — executable run holding `BacktestRequest`, mutable `BacktestRun` state, and an explicit final `Result`
- `Trader` — coordinates DataManager + Broker + Account + Journal
- `Broker` — executes `OpenRequest`/`closeRequest`, emits events on a channel
- `Strategy` interface — `Update(ctx, *market.CandleTime, StrategyContext) → Signal`
- `ExitStrategy` interface — optional exit logic layered on top of a strategy; see `strategy/exit.go`
- `RegimeFilter` interface — optional market-regime gate; see `strategy/regime.go`
- `DataManager` — candle cache and loader; `CandleIterator` for traversal
- `Journal` interface — write-only persistence contract with CSV and JSONL implementations

## Accounting Invariants

These must hold after every operation:
- `Equity = Balance + UnrealizedPL`
- `FreeMargin = Equity − MarginUsed`
- BUY: open at ask, close at bid; SELL: open at bid, close at ask
- P/L calculated in quote currency, then converted to account currency via `QuoteToAccount()`
- Stop/take-profit evaluated on **every price update** (inclusive)
- Automatic forced liquidation when `FreeMargin < 0` is not implemented yet. Do not assume
  that an account or backtest will liquidate itself under a margin call. Implementation is
  tracked in [GitHub issue #147](https://github.com/rustyeddy/trader/issues/147).

## Key Conventions

- **Config precedence:** root command merges `/etc/trader/*.yml`, `~/.config/trader/*.yml`,
  then an explicit `--config` file. OANDA-facing commands apply flags → global config → env
  vars → `~/.config/oanda/pat.txt` as the final token fallback.

- **Backtest outputs are stable:** report filenames are `<run-name>-<config-hash>`. Regression
  comparisons use exact numeric equality because the engine is built on scaled integers.

- **Backtest config shape:** top-level `defaults` cascade into each `runs[]` entry. Empty
  `exit` / `regime` sections intentionally resolve to `NoopExit` / `NoopRegime`.

- **Service boundary:** CLI handlers under `cmd/` and HTTP handlers under `api/rest` parse
  inputs, call typed `service` methods, and map results/errors at the edge. No business logic
  in transport layers.

- **Dead code:** if a symbol is only referenced by its own tests and not by any production code
  path, remove it along with its tests. Test-only references do not count as live usage.

## Configuration

Backtests are driven by YAML files. See `testdata/configs/` for examples. A config has:
- `defaults` — shared account, risk, execution-cost, scale, and source settings
- `runs[]` — named runs containing nested `data`, `strategy`, `exit`, and `regime` sections
- `runs[].data` — `instrument`, `timeframe`, `from`, `to`, and optional source/strict settings

The candle data root is global application configuration, not a backtest `defaults` field. It
defaults to `/srv/trading/data/candles` and can be overridden with `--data-dir` or global config.

## MCP Server

The repository includes a local stdio MCP server in `.mcp.json` named `trader`, backed by
`trader mcp serve`.

- Without an OANDA token: backtest-only tools available
- With `--token`: live account/trade tools enabled
- Write operations additionally require `--enable-write`

Standard local config shape:

```json
{
  "mcpServers": {
    "trader": {
      "type": "stdio",
      "command": "trader",
      "args": ["mcp", "serve"]
    }
  }
}
```

## Data Directory Layout

The default data root is `/srv/trading/data`. Two subtrees exist:

```
/srv/trading/data/
  raw/                          # unprocessed source data as downloaded
    oanda/<INSTRUMENT>/<YYYY>/<MM>/<INSTRUMENT>-<YYYY>-<MM>-<tf>.csv
    dukascopy/<INSTRUMENT>/<YYYY>/<MM>/<DD>/<INSTRUMENT>-<YYYY>-<MM>-<DD>-<HH>.bi5
  candles/                      # derived canonical OHLC candles (built by DataManager)
    oanda/<INSTRUMENT>/<YYYY>/<MM>/<INSTRUMENT>-<YYYY>-<MM>-<tf>.csv
  news_days/                    # news-day exclusion lists (text files, one date per line)
```

**Raw vs. derived:**
- `raw/` holds data exactly as it arrived from the source (OANDA CSV, Dukascopy bi5 tick files).
  Never write derived or processed data here.
- `candles/` holds canonical M1/H1/H4/D1 OHLC files built by `DataManager` from raw ticks or
  raw OANDA CSVs. These are what the backtest engine reads. Rebuild with `trader data build`.

**Instrument format in paths:** always the normalized form without underscores — `EURUSD`, not
`EUR_USD`. The store key (`datamanager/store_key.go`) enforces this; `NormalizeInstrument()` is applied
before any path construction.

**Timeframe suffixes in filenames:** `m1`, `h1`, `h4`, `d1`.

## Instrument Normalization

Two formats exist and must never be mixed up:

| Format | Example | Used where |
|--------|---------|------------|
| Normalized (no underscore) | `EURUSD` | Store keys, file paths, internal registry |
| OANDA wire format | `EUR_USD` | OANDA API requests and OANDA-specific live configuration |

`NormalizeInstrument(sym string) string` strips spaces, underscores, slashes, and uppercases.
Always call it before constructing a store key or looking up the instrument registry.
Never call it on values going out to the OANDA API — the API requires the underscore form.

The instrument registry (`market/instrument.go`) panics at init time if a key is not already
normalized, so mismatches are caught at startup rather than silently producing wrong results.

## Error Handling Patterns

**In `service/`:** return `error` using `fmt.Errorf("context: %w", err)` so callers can
unwrap with `errors.Is` / `errors.As`. Add enough context in the message that the error is
self-explanatory without a stack trace — include the relevant name, path, or key.

```go
return nil, fmt.Errorf("fetch %s: %w", monthStart.Format("2006-01"), err)
```

**In `cmd/` (CLI handlers):** print the error and call `os.Exit(1)` or return it to Cobra
(which prints and exits). Do not wrap again — the service error already has context.

**In `api/rest` (HTTP handlers):** map errors to HTTP status codes using `writeErr()`. Never
expose raw internal error strings to clients when they may contain sensitive paths or tokens;
use a generic message for 5xx and include detail only for 4xx (bad input).

```go
writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
writeErr(w, http.StatusInternalServerError, "run backtests: internal error")
writeJSON(w, http.StatusOK, result)
```

**Never `panic` in request paths.** Panics are reserved for programmer errors caught at
startup (e.g., malformed instrument registry entries). Use `log/slog` for structured logging.

## How to Add a New Strategy

1. **Copy the template** — `strategies/tmpl/` is a minimal working strategy skeleton. Copy it
   to `strategies/<name>/` as your starting point.

2. **Implement the `strategy.Strategy` interface:**
   ```go
   type Strategy interface {
       Name() string
       Ready() bool
       Reset()
       Update(ctx context.Context, ct *market.CandleTime, sc StrategyContext) Signal
       StopDescription() string
   }
   ```

3. **Register in `init()`:**
   ```go
   func init() {
       strategy.MustRegisterStrategy(build, "my-strategy-name")
   }
   ```
   where `build` is a function with signature `func(params map[string]any) (strategy.Strategy, error)`.

4. **Blank-import from `cmd/main.go`:**
   ```go
   _ "github.com/rustyeddy/trader/strategies/myname"
   ```
   Without this import the `init()` never runs and the strategy is invisible to the engine.

5. **Use only fixed-point types** inside the strategy. Indicators (`EMA`, `ATR`, `Bollinger`,
   etc.) all operate on `Price`; do not convert to float for intermediate calculations.

6. **Optional: implement `ExitStrategy` or `RegimeFilter`** if the strategy needs separable
   exit logic or a market-regime gate. See `strategy/exit.go` and `strategy/regime.go` for the
   interfaces; they are resolved by kind through the `GetExitStrategy` / `GetRegimeFilter`
   factories in `strategy/exit_factory.go` and `strategy/regime_factory.go`.

7. **Run `make sweep`** after adding the strategy to confirm it passes the broad smoke test
   across all registered strategies (`TestStrategySweep` in `service/`).

8. **Write tests** in `strategies/<name>/<name>_test.go` using synthetic candles from the
   `testdata` generators. Cover at minimum: not-ready state, first signal, stop logic.

## Testing Conventions

Every behavior change must ship with tests that demonstrate the new or corrected behavior.
Documentation-only, generated-artifact-only, and pure deletion changes do not require new tests.

- New functions, handlers, and types require focused unit tests for success, boundary, and error paths
- Modified functions must have their tests updated to cover the changed behaviour
- Run `make test` and `make cover` before committing; address gaps in coverage
- Use `testify` assertions (`require`, `assert`)
- Historical candle fixtures live in `testdata/candles/`
- Config fixtures live in `testdata/configs/`
- Synthetic candle generators exist for deterministic unit tests
- Tests are deterministic: same inputs always produce same outputs (no randomness)
- REST handlers: use `httptest.NewRecorder` + `httptest.NewRequest`; no real server needed
- Table-driven tests preferred for functions with multiple input/output cases

Choose validation based on the affected area:
- Go code: `gofmt`, `make vet`, `make lint`, and `make test`
- Strategy registration or behavior: Go checks plus `make sweep`
- UI code: `cd ui && npm run check` and `cd ui && npm run build`
- REST/MCP handlers: focused handler tests followed by `make test`
- Network and black-box checks: run only when the change requires them and credentials/data exist

## Safety and Generated Files

- Never place live OANDA orders, close live trades, run `make smoke-live`, or enable MCP write
  operations unless the user explicitly authorizes that live action.
- Treat `/srv/trading/data` as user-owned operational data. Tests must use temporary directories
  or committed fixtures and must not modify the live data store.
- `ui/dist/`, `ui/.svelte-kit/`, coverage files, generated CLI documentation, and generated
  reports are derived artifacts. Change their sources and regenerate them; do not hand-edit them.
- Network-hitting tests are opt-in. Normal unit tests must remain deterministic and offline.
