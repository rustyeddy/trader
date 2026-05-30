# Trader

A Go FX backtesting and live paper-trading engine with OANDA integration, a REST/WebUI, and Claude MCP tools.

---

## Install

```bash
git clone https://github.com/rustyeddy/trader
cd trader
make build          # → bin/trader
make install        # install to $GOPATH/bin
```

Requires Go 1.22+. No C dependencies.

---

## Quick Start

### Backtest

```bash
# Run a pre-built config against cached historical data
trader backtest --config testdata/configs/eurusd-h1-2024-ema-cross.yml

# Run all regression configs and write reports
trader backtest regress --config testdata/configs/
```

### Live Paper Trading

```bash
export OANDA_TOKEN=your-practice-api-token

# Dry-run: print resolved config and exit
trader live run --config testdata/configs/pulse-demo.yml --dry-run

# Run the pulse strategy against a practice account
trader live run --config testdata/configs/pulse-demo.yml
```

### Web UI + REST API

```bash
trader serve                         # REST on :9999, live journal, embedded UI
trader serve --addr :8080            # custom port
trader serve --log-level debug
```

Open `http://localhost:9999` for the dashboard.

---

## CLI Commands

| Command | Description |
|---|---|
| `trader backtest` | Run backtests against historical candles |
| `trader backtest regress` | Batch regression: run all configs, write JSON + org reports |
| `trader data sync` | Download ticks (Dukascopy) and build OHLC candles |
| `trader data oanda` | Download candles from OANDA into the candle store |
| `trader live run` | Run a single-instrument live strategy against OANDA |
| `trader live portfolio` | Run a multi-instrument live portfolio from a YAML config |
| `trader live journal` | Subscribe to OANDA transaction stream and journal closed trades |
| `trader order` | Place/close orders on a live OANDA account |
| `trader serve` | Long-running daemon: REST API + live journal + embedded UI |
| `trader replay` | Replay a dataset through the sim engine |
| `trader mcp` | Expose trader as typed Claude tools over stdio (MCP protocol) |

All commands accept `--help`.

---

## Backtesting

Backtests are driven by YAML config files. See `testdata/configs/` for a full library of examples.

```yaml
# testdata/configs/eurusd-h1-2024-ema-cross.yml (excerpt)
defaults:
  capital: 10000
  risk_pct: 1.0
  data_dir: /data/candles

runs:
  - instrument: EUR_USD
    start_date: 2024-01-01
    end_date:   2024-12-31
    strategy:
      name: emacross
      fast: 9
      slow: 21
```

Results are printed to stdout and optionally written to `reports/` as JSON.

---

## Live Trading

Live trading uses OANDA's REST API. A practice account is free at [oanda.com](https://www.oanda.com).

**Authentication** — set one of:
```bash
export OANDA_TOKEN=<your-token>        # env var
echo <token> > ~/.config/oanda/pat.txt # file fallback
```

**Config** (`testdata/configs/pulse-demo.yml`):
```yaml
instrument: EUR_USD
env: practice
tick_interval: 60s
max_positions: 1
risk_pct: 0.1
max_units: 5000         # hard cap on position size

strategy:
  kind: pulse
  params:
    trade_every: 5      # open every N ticks
    hold_bars: 15       # close after N ticks
    side: long
    stop_pips: 20
    risk_pct: 0.1
```

The runner polls prices every `tick_interval`, calls the strategy, executes closes then opens, and logs every action. `--log-level debug` adds per-trade tick counts and unrealized P/L each bar.

### Multi-Instrument Portfolio

Run multiple strategies concurrently with a shared drawdown circuit breaker:

```bash
trader live portfolio --config /path/to/demo-portfolio.yml --dry-run
trader live portfolio --config /path/to/demo-portfolio.yml
```

Portfolio YAML:

```yaml
env: practice
account_id: 101-001-XXXXXXX-001   # auto-discovered if omitted
risk_pct: 1.0                     # default risk per trade (%)
drawdown_circuit_pct: 10.0        # halt new opens if equity drops this % from peak
local_warmup_bars: 5000           # bars to load from local candle store for indicator priming

instruments:
  - instrument: EUR_USD
    timeframe: H1
    strategy:
      kind: donchianv6
    exit:
      kind: chandelier
      params: {period: 14, multiplier: 3.0}
    regime:
      kind: weeklyema
    risk_pct: 0.5
    max_units: 10000

  - instrument: GBP_USD
    timeframe: H1
    local_warmup_bars: 2000       # per-instrument override
    strategy:
      kind: emacross
    exit:
      kind: chandelier
      params: {period: 14, multiplier: 3.0}
```

### Indicator Warmup

Before emitting live signals the adapter primes all indicators (strategy, regime filter, exit/chandelier stop) using two phases:

1. **Local phase** — reads `local_warmup_bars` bars from the on-disk OANDA candle store. 500 bars covers ~3 weeks of H1 data; 5000 covers ~7 months and is sufficient for ATR-percentile and weekly-EMA regime filters.
2. **OANDA phase** — fetches the most recent ~100 bars from OANDA to bridge any gap between the newest local bar and now.

Set `local_warmup_bars: 0` to skip local warmup and use OANDA-only (the original behaviour).

### Signal Logging

Every strategy decision in the live path is emitted as a structured `slog` record:

| Event | Log level | Key fields |
|---|---|---|
| Strategy signals open | `INFO` | `instrument`, `side`, `stop`, `reason` |
| Regime filter blocks open | `INFO` | `instrument`, reason (`not trending` / `side not allowed`) |
| Open queued for OANDA | `INFO` | `instrument`, `side`, `entry_price`, `stop_price`, `stop_pips` |
| Strategy signals close | `INFO` | `instrument`, `count`, `reason` |
| No stop available — skipped | `ERROR` | `instrument`, `side`, `reason` |

Use `--log-level info` (the default) to see all signal events; `--log-level warn` to suppress them.

---

## Strategies

| Strategy | Description |
|---|---|
| `pulse` | Mechanical open/close on fixed tick schedule — useful for pipeline testing |
| `emacross` | EMA crossover (fast/slow) |
| `emacrossadx` | EMA crossover filtered by ADX trend strength |
| `donchian` | Donchian channel breakout |
| `noop` | Does nothing — baseline / benchmark |
| `fake` | Scripted actions for deterministic testing |
| `lifecycle` | Exercises the full open → modify-stop → close lifecycle |
| `tmpl` | Strategy template for new strategy development |

---

## Data Management

Historical data comes from two sources:

**Dukascopy** (tick data, free) — download and build candles:
```bash
trader data sync --instruments EUR_USD,GBP_USD --from 2022-01 --to 2024-12
```

**OANDA** (candles, requires token):
```bash
trader data oanda --instrument EUR_USD --granularity H1 --from 2024-01-01
```

Candle data is stored under `$DATA_DIR` (default `/data/candles`) in a hierarchy:
```
/data/candles/<source>/<INSTRUMENT>/<YYYY>/<MM>/
```

`testdata/candles/` contains small fixtures used by unit tests — do not use for real backtests.

---

## Architecture

The core backtest loop:

```
Config (YAML)
  → DataManager  (loads / caches OHLC candles)
  → Backtest     (iterates candles bar by bar)
  → Strategy     (returns StrategyPlan each bar)
  → Broker       (fills orders, emits Events)
  → Account      (updates equity, margin, P/L)
  → Journal      (records closed trades — CSV or SQLite)
```

**Numeric types** — all prices and money are fixed-point integers, never floats:

| Type | Scale | Notes |
|---|---|---|
| `Price` (int32) | 100,000 | 1.16177 → 116177 |
| `Money` (int64) | 1,000,000 | avoids float rounding |
| `Units` | 1 | position size in micro-lots |

**Accounting invariants** (must hold after every operation):
- `Equity = Balance + UnrealizedPL`
- `FreeMargin = Equity − MarginUsed`
- BUY: open at ask, close at bid; SELL: open at bid, close at ask
- Stop/take-profit evaluated on every bar (inclusive)
- Forced liquidation when `FreeMargin < 0`

---

## Testing

```bash
make test           # unit tests
make test-blackbox  # unit + REST API + MCP integration tests
make cover          # coverage report (stdout)
make cover-html     # coverage report (browser)

# Run a single test
go test -run TestName ./...

# Enable Dukascopy download tests (hits network)
TRADER_RUN_DUKASCOPY_TESTS=1 go test ./...
```

Every code change must ship with tests — see `CLAUDE.md` for conventions.

---

## Project Layout

```
cmd/            CLI entry points (Cobra)
api/rest/       REST handlers
api/mcp/        Claude MCP tool server
brokers/oanda/  OANDA REST client
service/        Business logic (orders, live runner, journal)
strategies/     Strategy implementations
data/           Candle loading, Dukascopy parser
ui/             Embedded SvelteKit frontend
testdata/       Config fixtures and candle fixtures
ROADMAP.md      Planned features and known gaps
```

---

## Strategy Replay

The replay API runs any strategy against stored local candles and returns every bar plus a full signal log — without placing any orders. Use it to debug signal generation, visualise where entries and stops were placed, and tune parameters.

### REST API

```bash
curl -s -X POST http://localhost:9999/api/v1/replay \
  -H 'Content-Type: application/json' \
  -d '{
    "instrument":   "EURUSD",
    "timeframe":    "H1",
    "from":         "2026-01-01",
    "to":           "2026-05-29",
    "warmup_bars":  200,
    "strategy":     {"kind": "donchianv6"},
    "exit":         {"kind": "chandelier", "params": {"period": 14, "multiplier": 3.0}},
    "regime":       {"kind": "weeklyema"}
  }'
```

Response shape:

```json
{
  "instrument": "EURUSD",
  "timeframe":  "H1",
  "strategy":   "donchian-v6",
  "bars":    [ { "time": 1234567890, "open": 1.1, "high": 1.11, "low": 1.09, "close": 1.105 } ],
  "signals": [
    { "time": 1234567890, "kind": "open",        "side": "long", "price": 1.105, "stop_price": 1.095, "stop_pips": 10.0, "reason": "breakout" },
    { "time": 1234571490, "kind": "stop_update", "side": "long", "stop_price": 1.098 },
    { "time": 1234575090, "kind": "close",        "side": "long", "price": 1.098, "reason": "stop hit" },
    { "time": 1234578690, "kind": "blocked",      "side": "long", "reason": "regime: not trending" }
  ]
}
```

Signal kinds:

| Kind | Meaning |
|---|---|
| `open` | Strategy signalled an entry; includes `stop_price` and `stop_pips` |
| `close` | Strategy signalled an exit |
| `stop_update` | Chandelier trailing stop ratcheted to a new level |
| `blocked` | Regime filter suppressed an open signal |
| `no_stop` | Open skipped — strategy produced no stop and exit strategy not ready |

### Web UI

Open `http://localhost:9999/replay` for the visual replay interface. Controls:

- **Instrument / Timeframe / From / To** — the data window
- **Strategy** — which strategy to run (donchianv6, emacross, bollingerfade, …)
- **Exit** — chandelier ATR trailing stop (ATR period + multiplier)
- **Regime** — optional trend filter (Weekly EMA, ATR Percentile, D1 ADX, …)
- **Warmup bars** — indicator priming bars before recording signals

The chart renders:
- **Green ▲ / Red ▼** entry markers with stop-pips label
- **Gray ●** exit markers
- **Yellow ■** regime-blocked signals
- **Orange ■** no-stop-dropped signals
- **Dashed orange line** — chandelier stop trail tracking from entry to exit

---

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned features including walk-forward testing, external/plugin strategies, and more.
