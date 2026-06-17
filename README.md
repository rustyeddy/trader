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

Requires Go 1.22+.

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

# Single instrument against a practice account
trader live run --config testdata/configs/pulse-demo.yml

# Multi-instrument portfolio
trader live portfolio --config /path/to/portfolio.yml --dry-run
trader live portfolio --config /path/to/portfolio.yml
```

### Daemon (REST API + UI)

```bash
trader serve --config deploy/trader.yaml.example   # REST on :9999, embedded UI, live journal
trader serve --addr :8080 --log-level debug
```

Open `http://localhost:9999` for the dashboard.

---

## CLI Commands

| Command | Description |
|---|---|
| `trader analysis` | Parse a ChatGPT forex analysis CSV and print trade candidates and watchlist |
| `trader backtest` | Run backtests against historical candles |
| `trader backtest regress` | Batch regression: run all configs, write JSON + org reports |
| `trader data sync` | Download ticks (Dukascopy) and build OHLC candles |
| `trader data oanda` | Download candles directly from OANDA into the candle store |
| `trader data candles` | Print local candles in canonical CSV format |
| `trader data validate-candles` | Scan local candle months for missing expected bars and raw-source mismatches |
| `trader data stats` | Print statistics for a historical candle dataset |
| `trader data pip-value` | Show USD value of 1/10/100/1000 pips for each major pair |
| `trader data position` | Convert between position size, USD notional value, and pip P&L |
| `trader order account` | Print OANDA account balance, NAV, margin, and unrealized P/L |
| `trader order update-stop` | Update stop-loss and/or take-profit on an open trade |
| `trader live run` | Run a single-instrument live strategy against OANDA |
| `trader live portfolio` | Run a multi-instrument live portfolio from a YAML config |
| `trader order prices` | Fetch live bid/ask prices from OANDA for the major pairs |
| `trader live journal` | Subscribe to OANDA transaction stream and journal closed trades |
| `trader order` | Place, close, and list orders on a live OANDA account |
| `trader serve` | Full daemon: REST API + live journal + embedded UI (port :9999) |
| `trader api serve` | Minimal REST API only, no journal (port :8080) |
| `trader replay` | Replay a dataset through the sim engine |
| `trader mcp` | Expose trader as typed Claude tools over stdio (MCP protocol) |

All commands accept `--help`.

Live journaling defaults to newline-delimited JSON files (`*.jsonl`) for trades and equity snapshots so the records stay easy to inspect now and easy to import into a database later.

---

## Backtesting

Backtests are driven by YAML config files. See `testdata/configs/` for a full library of examples.

```yaml
# testdata/configs/eurusd-h1-2024-ema-cross.yml (excerpt)
defaults:
  capital: 10000
  risk_pct: 1.0
  data_dir: /srv/trading/data/candles

runs:
  - instrument: EUR_USD
    start_date: 2024-01-01
    end_date:   2024-12-31
    strategy:
      name: ema-cross
      fast: 9
      slow: 21
```

Results are printed to stdout and optionally written to `reports/` as JSON + org-mode files.

---

## Live Trading

Live trading uses OANDA's REST API. A practice account is free at [oanda.com](https://www.oanda.com).

**Authentication** — set one of:
```bash
export OANDA_TOKEN=<your-token>        # env var (preferred)
echo <token> > ~/.config/oanda/pat.txt # file fallback
```

### Single Instrument

Config (`testdata/configs/pulse-demo.yml`):
```yaml
instrument: EUR_USD
env: practice           # practice | live
tick_interval: 60s      # how often to poll prices
max_positions: 1
risk_pct: 0.1           # % of account NAV to risk per trade
max_units: 5000         # hard unit cap
max_position_usd: 0     # hard notional cap in account currency (0 = none)

strategy:
  kind: pulse
  params:
    trade_every: 5      # open every N ticks
    hold_bars: 15       # close after N ticks
    side: long
    stop_pips: 20
    risk_pct: 0.1
```

```bash
trader live run --config testdata/configs/pulse-demo.yml
trader live run --config testdata/configs/pulse-demo.yml --env live --instrument GBP_USD
```

### Multi-Instrument Portfolio

Run multiple strategies concurrently with a shared drawdown circuit breaker:

```yaml
env: practice
account_id: 101-001-XXXXXXX-001   # auto-discovered if omitted
risk_pct: 1.0                     # default risk per trade (%)
drawdown_circuit_pct: 10.0        # halt new opens if equity drops this % from peak
local_warmup_bars: 5000           # bars to load from local store for indicator priming

instruments:
  - instrument: EUR_USD
    timeframe: H1
    tick_interval: 60s            # poll interval (optional, inherits global default)
    risk_pct: 0.5                 # overrides top-level default
    max_units: 10000

    strategy:
      kind: donchian-v6

    exit:
      kind: chandelier
      params:
        atr_period: 14
        multiplier: 3.0

    regime:
      kind: weekly-ema

  - instrument: GBP_USD
    timeframe: H1
    local_warmup_bars: 2000       # per-instrument override
    strategy:
      kind: ema-cross
    exit:
      kind: chandelier
      params: {atr_period: 14, multiplier: 3.0}
```

```bash
trader live portfolio --config portfolio.yml --dry-run
trader live portfolio --config portfolio.yml
```

### Indicator Warmup

Before emitting live signals the adapter primes all indicators (strategy, regime filter, chandelier stop) using two phases:

1. **Local phase** — reads `local_warmup_bars` bars from the on-disk OANDA candle store. 500 bars covers ~3 weeks of H1 data; 5000 covers ~7 months — sufficient for ATR-percentile and weekly-EMA regime filters.
2. **OANDA phase** — fetches the most recent ~100 bars from OANDA to bridge any gap between the newest local bar and now.

Set `local_warmup_bars: 0` to skip local warmup and use OANDA-only.

### Signal Logging

All three event types — strategy signals, broker fills, and OANDA-initiated closes — flow through the same structured `slog` stream. With `--log-level info` (the default) every trading event is captured in one place.

| Source | Message | Key fields |
|---|---|---|
| Strategy | `live: strategy signal open` | `instrument`, `side`, `stop`, `reason` |
| Strategy | `live: open blocked by regime filter` | `instrument`, `side`, reason (`not trending` / `side not allowed`) |
| Strategy | `live: open order queued` | `instrument`, `side`, `entry_price`, `stop_price`, `stop_pips` |
| Strategy | `live: strategy signal close` | `instrument`, `count`, `reason` |
| Strategy | `candle adapter: strategy returned open with no stop` | `instrument`, `side`, `reason` |
| Broker fill | `live runner: opened trade` | `trade_id`, `side`, `units`, `price` (OANDA confirmed fill) |
| Broker fill | `live runner: closed trade` | `trade_id` (strategy-triggered close) |
| Stop-out / TP | `live-journal trade recorded` | `trade_id`, `instrument`, `entry`, `exit`, `pl`, `reason` |

The `reason` field on `live-journal trade recorded` contains the OANDA close reason:
- `STOP_LOSS_ORDER` — stop-loss hit
- `TAKE_PROFIT_ORDER` — take-profit hit
- `CLIENT_REQUEST` — closed manually via the API

**Configuration** — add to `trader.yaml` or pass as flags:

```yaml
log:
  level: info     # debug | info | warn | error
  format: json    # json enables structured filtering with jq
  file: /var/log/trader/trader.log   # written in addition to stdout
```

```bash
# Flags override the config file
trader serve --log-level info --log-format json --log-file /var/log/trader/trader.log
```

**Filter the live log with jq** (requires `--log-format json`):

```bash
# Tail all trading events — skip tick-level noise
tail -f /var/log/trader/trader.log | jq -c 'select(.msg | test("signal|queued|opened trade|closed trade|journal trade"))'

# Entries only — with stop price and pips
tail -f /var/log/trader/trader.log | jq -c 'select(.msg == "live: open order queued") | {time, instrument, side, entry_price, stop_price, stop_pips}'

# Fills only
tail -f /var/log/trader/trader.log | jq -c 'select(.msg == "live runner: opened trade") | {time, trade_id, side, units, price}'

# Stop-outs and closes with P/L
tail -f /var/log/trader/trader.log | jq -c 'select(.msg == "live-journal trade recorded") | {time, trade_id, instrument, entry, exit, pl, reason}'

# Everything in one clean stream
tail -f /var/log/trader/trader.log | \
  jq -c 'select(.msg | test("queued|opened trade|closed trade|journal trade")) |
         {time, msg: (.msg | split(":")[1] | ltrimstr(" ")), instrument, side,
          entry_price, stop_price, stop_pips, trade_id, price, pl, reason}'
```

---

## Strategies

Strategies are referenced by their registered kind string in config files.

| Kind | Description | Live? |
|---|---|---|
| `pulse` | Mechanical open/close on fixed tick schedule — useful for pipeline testing | live only |
| `ema-cross` | EMA crossover (fast/slow periods configurable) | backtest + live |
| `ema-cross-adx` | EMA crossover filtered by ADX trend strength | backtest + live |
| `donchian` | Donchian channel breakout (v1) | backtest + live |
| `donchian-v2` | Donchian v2 with improved exit logic | backtest + live |
| `donchian-v3` | Donchian v3 | backtest + live |
| `donchian-v4` | Donchian v4 | backtest + live |
| `donchian-v5` | Donchian v5 | backtest + live |
| `donchian-v6` | Donchian v6 — most recent, recommended | backtest + live |
| `bb-fade` | Bollinger Band fade (mean-reversion) | backtest + live |
| `noop` | Does nothing — baseline / benchmark | backtest + live |
| `fake` | Scripted actions for deterministic testing | backtest only |
| `lifecycle-test` | Exercises the full open → modify-stop → close lifecycle | backtest only |
| `template` | Starter template for new strategy development | backtest only |

### Exit Strategies

Exit strategies control the trailing stop. Configured via the `exit:` block in portfolio YAML or used implicitly by the backtest engine.

| Kind | Description |
|---|---|
| `chandelier` | ATR-based chandelier trailing stop. Params: `atr_period` (default 14), `multiplier` (default 3.0) |
| `""` / `noop` | No trailing stop — strategy sets its own fixed stop |

### Regime Filters

Regime filters suppress entries when the market is not in a favourable state.

| Kind | Description |
|---|---|
| `""` / `noop` | No filtering — all signals pass through |
| `weekly-ema` | Allow longs only above weekly EMA, shorts only below |
| `atr-percentile` | Block entries when ATR is below a percentile threshold (range-bound markets) |
| `adx-d1` | Block entries when daily ADX is below threshold (no trend) |
| `choppiness` | Block entries when choppiness index signals sideways price action |
| `choppiness-d1` | Same as above using daily bars |
| `session` | Allow entries only during specified trading sessions |
| `composite` | Combine multiple filters (all must pass); use `filters:` list in config |

---

## Data Management

Historical data comes from two sources:

**Dukascopy** (tick data, free) — download and build candles:
```bash
trader data sync --instruments EUR_USD,GBP_USD --from 2022-01 --to 2024-12
```

**OANDA** (candles, requires token):
```bash
# All flags are required
trader data oanda \
  --instrument EUR_USD \
  --timeframe  H1 \
  --from       2024-01-01 \
  --to         2024-12-31 \
  --env        practice
```

Candle data is stored under `--data-dir` (default `/srv/trading/data/candles`) in a hierarchy:
```
/srv/trading/data/candles/<source>/<INSTRUMENT>/<YYYY>/<MM>/
```

When OANDA candles are downloaded with raw preservation enabled, the bid+ask source rows are also written under the sibling raw tree:

```
/srv/trading/data/raw/oanda/<INSTRUMENT>/<YYYY>/<MM>/
```

`testdata/candles/` contains small fixtures used by unit tests — do not use for real backtests.

### Candle Completeness and Validation

Monthly candle files are no longer treated as complete just because the CSV exists and is non-empty. Inventory scanning reads the candle validity bits and marks a month incomplete if expected open-market slots are missing. Closed-market periods are allowed; missing bars during expected trading windows are not.

Use `trader data validate-candles` to scan stored months and optionally compare canonical OANDA candle coverage with preserved raw OANDA monthly files:

```bash
trader data validate-candles \
  --instruments EURUSD,USDJPY \
  --timeframe H1 \
  --from 2026-01 \
  --to 2026-03 \
  --source oanda \
  --check-raw \
  --report /tmp/candle-validation.json
```

What it reports:

| Issue kind | Meaning |
|---|---|
| `missing_candle_month` | The canonical monthly candle CSV is missing entirely |
| `missing_expected_candles` | Expected open-market bars are missing from the month |
| `invalid_candles` | Present bars have invalid OHLC shape |
| `missing_raw_source` | Raw OANDA monthly preservation file is missing |
| `raw_complete_missing_canonical` | Raw OANDA has complete bars that are absent from canonical candles |
| `canonical_missing_raw_complete` | Canonical candles contain valid bars not backed by raw OANDA complete rows |

The command prints a summary to stdout and, with `--report`, writes a JSON report containing per-month counts, paths, and sample missing timestamps. This is the easiest way to keep an auditable record of gaps that should exist but do not.

### Candle CSV Export

Raw local candle reads go through `Service.CandlesCSV`, which streams candles from the canonical store and returns the same scaled integer CSV format used on disk. The service is shared by CLI, REST, and MCP so callers get consistent output:

```csv
# schema=v1 source=oanda instrument=EURUSD tf=h1 scale=100000
Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags
1704067200,110100,110000,109900,110050,10,15,60,0x0001
```

CLI:

```bash
trader data candles \
  --instrument EUR_USD \
  --timeframe  H1 \
  --from       2024-01-01 \
  --to         2024-01-31
```

`--to` is optional and defaults to now/latest available. Dates are inclusive at the caller boundary. Prices and spreads are emitted as fixed-point scaled integers, not floats.

### Dataset Statistics

`trader data stats` walks a candle dataset and reports four groups of metrics:

| Group | What it measures |
|---|---|
| **Swing** | High-low range per bar: count, mean, min, p25/p50/p75/p90, max (in pips) |
| **Spread** | Average spread per bar: mean, p90, max (in pips; bars with zero spread are skipped) |
| **Trend vs Consolidation** | Body/range ratio — `\|Close−Open\| / (High−Low)`. >0.6 = trending, <0.3 = consolidating |
| **Session** | Average range and bar count by UTC hour — shows which sessions are most active |

```bash
# Pips only
trader data stats \
  --instrument EURUSD \
  --timeframe  H1 \
  --from       2020-01-01 \
  --to         2024-12-31

# Pips + USD value for a standard lot (100,000 units)
trader data stats --instrument EURUSD --from 2020-01-01 --to 2024-12-31 --units 100000
```

`--units` adds a USD column showing what each pip measurement is worth at the given position size. Position sizes: `1000` = micro lot, `10000` = mini lot, `100000` = standard lot. For USD-base pairs (USDJPY, USDCHF, USDCAD) approximate rates are used automatically.

Example output with `--units 100000`:
```
EURUSD H1   2020-01-01 → 2024-12-31   (USD at standard lot)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Swing (High-Low Range)
  count                      21890
  mean                       14.3 pips  ($143.00)
  min                         0.1 pips  ($1.00)
  p25                         8.1 pips  ($81.00)
  p50                        12.4 pips  ($124.00)
  p75                        18.9 pips  ($189.00)
  p90                        26.7 pips  ($267.00)
  max                       112.0 pips  ($1120.00)

Spread
  count (with spread)        21890
  mean                        0.18 pips  ($1.80)
  p90                         0.30 pips  ($3.00)
  max                         2.10 pips  ($21.00)

Trend vs Consolidation
  count                      21890
  mean body/range             0.421
  trending  (>0.6)           35.2%  (7705)
  mixed  (0.3–0.6)           34.8%  (7618)
  consolidating  (<0.3)      30.0%  (6567)

Session (by UTC hour)
  00:00 UTC                  count=1094    avg range=8.3 pips  ($83.00)
  01:00 UTC                  count=1089    avg range=7.9 pips  ($79.00)
  ...
  08:00 UTC                  count=1096    avg range=15.2 pips  ($152.00)
  09:00 UTC                  count=1098    avg range=18.4 pips  ($184.00)
  ...
```

`--timeframe` defaults to `H1`. All three timeframes (`M1`, `H1`, `D1`) are supported. `--from` and `--to` are both inclusive.

### Pip Values

`trader data pip-value` prints the USD value of 1, 10, 100, and 1000 pips for every major pair at a given position size:

```bash
# Default: 100,000 units (1 standard lot), approximate rates for USD-base pairs
trader data pip-value

# Mini lot with live rates
trader data pip-value --units 10000 --rates USDJPY=152.50,USDCHF=0.88,USDCAD=1.38
```

Example output:
```
Pip values — 100,000 (standard lot) units  (USD per N pips)

Instrument       1 pip     10 pips    100 pips     1000 pips
──────────  ──────────  ──────────  ──────────  ────────────
EURUSD          $10.00    $100.00     $1,000    $10,000
GBPUSD          $10.00    $100.00     $1,000    $10,000
USDJPY    †    $6.6667     $66.67    $666.67     $6,667
USDCHF    †     $11.11    $111.11     $1,111    $11,111
AUDUSD          $10.00    $100.00     $1,000    $10,000
USDCAD    †    $7.3529     $73.53    $735.29     $7,353
NZDUSD          $10.00    $100.00     $1,000    $10,000

† approximate rate(s): USDJPY=150, USDCHF=0.9, USDCAD=1.36
  Override with --rates USDJPY=152.50,USDCHF=0.88,USDCAD=1.38
```

USD-quoted pairs (EURUSD, GBPUSD, AUDUSD, NZDUSD) are exact and need no rate. USD-base pairs (USDJPY, USDCHF, USDCAD) are marked `†` and use approximate defaults until you supply `--rates`.

---

## REST API

`trader serve` (port :9999) exposes the following endpoints. Most return JSON; the raw candle export returns `text/csv`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Health check |
| `GET` | `/api/v1/account` | OANDA account summary (balance, NAV, margin, unrealized P/L) |
| `GET` | `/api/v1/prices` | Live bid/ask prices and spread in pips (`?instruments=EURUSD,GBPUSD`, default all majors) |
| `GET` | `/api/v1/trades` | Open trades |
| `POST` | `/api/v1/trades` | Place a risk-sized market order |
| `PATCH` | `/api/v1/trades/{id}/stop` | Update stop / take-profit on an open trade |
| `DELETE` | `/api/v1/trades/{id}` | Close a trade (full or partial) |
| `GET` | `/api/v1/transactions` | OANDA transaction history (`?since_id=N`) |
| `GET` | `/api/v1/candles/{instrument}` | Local candles as canonical CSV (`from`, `to`, `timeframe`, optional `source`) |
| `GET` | `/api/v1/candles/{instrument}/stats` | Candle dataset statistics — swing, spread, trend, session (`from`, `to`, `timeframe`, `units`) |
| `GET` | `/api/v1/candles/validate` | Validate local candle store for gaps and raw-source mismatches (`instruments`, `from`, `to`, `timeframe`) |
| `GET` | `/api/v1/pip-values` | USD pip values for major pairs (`?units=100000`, `?instruments=EURUSD,USDJPY`) |
| `GET` | `/api/v1/position` | Position sizing table — notional, margin, pip P&L (`?instrument=EURUSD&price=1.08&units=100000&pips=20`) |
| `POST` | `/api/v1/backtests/run` | Run one or more backtest configs |
| `GET` | `/api/v1/backtests` | List saved backtest reports |
| `GET` | `/api/v1/backtests/{name}` | Get a single backtest report |
| `GET` | `/api/v1/backtests/{name}/candles` | OHLC bars for a saved report |
| `POST` | `/api/v1/replay` | Run a strategy replay; returns bars + signal log |
| `POST` | `/api/v1/analysis` | Parse a ChatGPT forex analysis CSV upload; returns rows split by status |
| `GET` | `/api/v1/stream/account` | SSE: account equity stream |
| `GET` | `/api/v1/stream/events` | SSE: broker event stream |
| `GET` | `/api/v1/stream/backtest/{id}` | SSE: live backtest progress |

OANDA endpoints return `503` when the server starts without a token (backtest-only mode).

Example candle CSV request:

```bash
curl -s 'http://localhost:9999/api/v1/candles/EUR_USD?from=2024-01-01&to=2024-01-31&timeframe=H1'
```

---

## MCP Tools

`trader mcp serve` exposes typed tools over stdio. Tools that read local data or perform pure calculations work without an OANDA token. Live account and trade tools require `--token`. Write tools (`download_candles`, `place_order`, `close_trade`, `update_stop`) also require `--enable-write`.

| Tool | Needs OANDA | Write? | Description |
|---|---|---|---|
| `get_account_summary` | yes | — | Account balance, NAV, margin, unrealized P/L |
| `get_prices` | yes | — | Live bid/ask and spread in pips for major pairs |
| `list_open_trades` | yes | — | All open positions |
| `get_transactions` | yes | — | Transaction history since a given ID |
| `get_candles_csv` | no | — | Local candles in canonical CSV |
| `get_candle_stats` | no | — | Swing, spread, trend, session statistics for a candle dataset |
| `validate_candles` | no | — | Scan stored months for gaps and raw-source mismatches |
| `get_pip_values` | optional | — | USD pip values for major pairs (live rates when OANDA available) |
| `get_position` | optional | — | Position sizing — notional, margin, pip P&L (live price when OANDA available) |
| `run_backtest` | no | — | Run backtest configs and return summaries |
| `download_candles` | yes | yes | Download and store OANDA candles |
| `place_order` | yes | yes | Size and submit a risk-based market order |
| `close_trade` | yes | yes | Close an open trade fully or partially |
| `update_stop` | yes | yes | Update stop-loss and/or take-profit on an open trade |

Local config example:

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

---

## Strategy Replay

The replay API runs any strategy against stored local candles and returns every bar plus a full signal log — without placing any orders. Use it to debug signal generation, visualise where entries and stops were placed, and tune parameters interactively.

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
    "strategy":     {"kind": "donchian-v6"},
    "exit":         {"kind": "chandelier", "params": {"atr_period": 14, "multiplier": 3.0}},
    "regime":       {"kind": "weekly-ema"}
  }'
```

Response includes `bars[]` (OHLC) and `signals[]`. Signal kinds:

| Kind | Meaning |
|---|---|
| `open` | Strategy signalled an entry; includes `stop_price` and `stop_pips` |
| `close` | Strategy signalled an exit |
| `stop_update` | Chandelier trailing stop ratcheted to a new level |
| `blocked` | Regime filter suppressed an open signal |
| `no_stop` | Open skipped — strategy produced no stop and exit strategy not ready |

Save the response and slice it with `jq` to analyse signals offline:

```bash
# Save replay output to file
curl -s -X POST http://localhost:9999/api/v1/replay \
  -H 'Content-Type: application/json' \
  -d '{
    "instrument": "EURUSD", "timeframe": "H1",
    "from": "2026-01-01", "to": "2026-05-29",
    "warmup_bars": 200,
    "strategy": {"kind": "donchian-v6"},
    "exit":     {"kind": "chandelier", "params": {"atr_period": 14, "multiplier": 3.0}},
    "regime":   {"kind": "weekly-ema"}
  }' > replay.json

# Signal summary
jq '.signals | group_by(.kind) | map({(.[0].kind): length}) | add' replay.json

# All entries with human-readable time and stop distance
jq '[.signals[] | select(.kind == "open")] |
    map({time: (.time | todate), side, price, stop_price, stop_pips, reason})' replay.json

# All exits
jq '[.signals[] | select(.kind == "close")] |
    map({time: (.time | todate), side, price, reason})' replay.json

# Blocked signals (regime filter)
jq '[.signals[] | select(.kind == "blocked")] |
    map({time: (.time | todate), side, reason})' replay.json

# Chronological timeline — skip stop_update noise
jq '[.signals[] | select(.kind != "stop_update")] |
    map({time: (.time | todate), kind, side, price, stop_pips, reason})' replay.json
```

### Web UI

Open `http://localhost:9999/replay`. Controls: instrument, timeframe, date range, strategy, exit strategy (ATR period + multiplier), regime filter, warmup bars. Click **Run Replay** to render:

- **Green ▲ / Red ▼** entry markers with stop-pips label
- **Gray ●** exit markers
- **Yellow ■** regime-blocked signals
- **Orange ■** no-stop-dropped signals
- **Dashed orange line** — chandelier stop trail from entry to exit

The signal summary bar below the controls shows counts for each kind. The chart re-renders immediately when you change parameters and click Run again — useful for tuning the ATR multiplier or switching regime filters interactively.

---

## Analysis

The analysis feature reads a ChatGPT-generated forex analysis CSV and classifies each pair as a trade candidate, watchlist item, or no-trade.

### CSV Format

The CSV must have these nine columns (row 1 is a header):

| Column | Example |
|---|---|
| Group | `Major Pairs` |
| Pair | `EUR/USD` |
| Structure | `Near 1.1590; USD softer after risk-on headline…` |
| Setup Bias | `Breakout continuation only after clean 4H close…` |
| Trend | `Bullish EUR / bearish USD` |
| Volatility | `Medium-High` |
| Support zone | `1.1570–1.1590` |
| Resistance Zone | `1.1600–1.1625` |
| Status | `Tradeable watch list` \| `Watchlist` \| `No Trade` |

Support and resistance zones are price ranges separated by an en dash (`–`), em dash (`—`), or hyphen.

### CLI

```bash
# Print watchlist and trade candidates (No Trade rows hidden by default)
trader analysis --file forex_analysis_2026-06-15.csv

# Include No Trade rows
trader analysis --file forex_analysis_2026-06-15.csv --all
```

Example output:
```
PAIR     STATUS                TREND                      VOLATILITY   SUPPORT          RESISTANCE
----     ------                -----                      ----------   -------          ----------
EUR/USD  Tradeable watch list  Bullish EUR / bearish USD  Medium-High  1.1570–1.1590    1.1600–1.1625
GBP/USD  Watchlist             Mild bullish GBP           High         1.3400–1.3410    1.3420–1.3460
AUD/USD  Tradeable watch list  Mild bullish AUD           High         0.7050–0.7070    0.7080–0.7100
EUR/CAD  Tradeable watch list  Mild bullish EUR           Medium-High  1.6150–1.6210    1.6260–1.6320
```

### REST API

`POST /api/v1/analysis` accepts a multipart form upload with field name `file` and returns the parsed rows pre-partitioned into three slices.

```bash
curl -s -X POST http://localhost:9999/api/v1/analysis \
  -F "file=@forex_analysis_2026-06-15.csv"
```

Response:
```json
{
  "total": 19,
  "tradeable": [
    {
      "group": "Major Pairs",
      "pair": "EUR/USD",
      "structure": "Near 1.1590; USD softer after risk-on headline…",
      "setup_bias": "Breakout continuation only after clean 4H close…",
      "trend": "Bullish EUR / bearish USD",
      "volatility": "Medium-High",
      "support_low": 1.157,
      "support_high": 1.159,
      "resistance_low": 1.16,
      "resistance_high": 1.1625,
      "status": "Tradeable watch list"
    }
  ],
  "watchlist": [ ... ],
  "no_trade":  [ ... ]
}
```

---

## Deployment

### Docker

```bash
cp deploy/env.example .env
# edit .env: OANDA_TOKEN, OANDA_ACCOUNT_ID, INSTRUMENT, STRATEGY

# Start the live bot + Postgres services
docker compose up -d live postgres

# Run a one-off backtest
docker compose run --rm backtest

# Download candles
docker compose run --rm data

# Raspberry Pi (adds memory caps + NFS candle volume)
docker compose -f docker-compose.yml -f deploy/docker-compose.pi.yml up -d live
```

### Systemd

A ready-to-use unit file is at `deploy/trader.service`. It runs `trader serve` with the config at `/etc/trader/trader.yaml`. Copy the example config:

```bash
sudo cp deploy/trader.yaml.example /etc/trader/trader.yaml
sudo cp deploy/trader.service /etc/systemd/system/
sudo systemctl enable --now trader
```

---

## Architecture

The core backtest loop:

```
Config (YAML)
  → DataManager  (loads / caches OHLC candles)
  → Backtest     (iterates candles bar by bar)
  → Strategy     (returns StrategyPlan each bar)
  → ExitStrategy (computes / updates trailing stop)
  → RegimeFilter (suppresses entries in ranging markets)
  → Broker       (fills orders, emits Events)
  → Account      (updates equity, margin, P/L)
  → Journal      (records closed trades — CSV or JSON)
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

Every code change must ship with tests — see `docs/CLAUDE.md` for conventions.

### Live Integration Smoke Test

`make smoke-live` runs the pulse strategy against an OANDA practice account to exercise the full broker plumbing at high frequency. Requires an active market session (London/NY overlap: 13:00–17:00 UTC recommended) and `OANDA_TOKEN` set in the environment.

```bash
export OANDA_TOKEN=your-practice-token

make smoke-live-dry   # parse and resolve config only — no orders placed
make smoke-live       # full run; logs to logs/smoke-live.log

# Tail trading events while running
tail -f logs/smoke-live.log | jq -c 'select(.msg | test("signal|opened trade|closed trade|journal trade"))'
```

Config: `testdata/configs/smoke-test.yml` — EUR_USD M1 pulse, trades every ~90s, 15-pip stops, session-gated to 13:00–17:00 UTC. Uncomment the `GBP_USD` block to test multi-instrument concurrency (phase 2).

| Target | Needs OANDA? | What it does |
|---|---|---|
| `make smoke` | No | Offline CI: build, backtest, replay API |
| `make smoke-live-dry` | Token only | Resolve config, print plan, exit |
| `make smoke-live` | Token + open session | Full pulse run, JSON log |

---

## Project Layout

```
cmd/            CLI entry points (Cobra)
cmd/analysis/   ChatGPT forex analysis CSV parser and classifier
api/rest/       REST handlers and routing
api/mcp/        Claude MCP tool server
brokers/oanda/  OANDA REST + streaming client
service/        Business logic (orders, candle CSV export, live runner, replay, journal)
strategies/     Strategy implementations
data/           Candle loading, Dukascopy parser
ui/             Embedded SvelteKit frontend (build → ui/dist/)
deploy/         Dockerfile, docker-compose, systemd unit, example configs
testdata/       Config fixtures and candle fixtures
lots-of.go	    Trader core source code
docs/           Project notes, roadmap, service docs, and plans
```

---

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for planned features including walk-forward testing, external/plugin strategies, and more.
