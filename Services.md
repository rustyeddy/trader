# trader — Service Reference

## Overview

**trader** is a Go FX (forex) backtesting and live-trading engine that
targets the OANDA v20 REST API. It is structured in layers: the
**CLI** (`trader <subcommand>`) is the entry point for every
operation; the **service** package holds all business logic and is
broker-agnostic; the **REST API** and **MCP server** expose the same
service methods over HTTP and stdio respectively; and **SSE streams**
push live account and market data to browser clients or other
consumers. The daemon mode (`trader serve`) runs the REST API and a
live OANDA transaction journal side-by-side, with exponential-backoff
reconnect, under a single process.

---

## CLI

Binary: `trader`

Global flags (available on every subcommand):

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | _(none)_ | Path to a YAML config file or directory |
| `--db` | `./trader.db` | SQLite journal database path |
| `--report` | _(none)_ | Backtest report output path |
| `--data-dir` | `/data/candles` | Root directory for candle data |
| `--log-level` | `debug` | Log level: `debug\|info\|warn\|error` |
| `--no-color` | `false` | Disable colored output |

---

### `trader version`

Print version information.

```
trader version
```

---

### `trader serve`

Run trader as a long-running daemon. Boots structured logging, a warm
candle cache, an OANDA broker connection, a live transaction-stream
journal (with exponential-backoff reconnect), and the REST API
server. Graceful shutdown on SIGTERM / SIGINT.

```
trader serve [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | _(none)_ | Path to YAML daemon config file |
| `--addr` | `:9999` | REST API listen address |
| `--token` | `$OANDA_TOKEN` | OANDA API token |
| `--account-id` | `$OANDA_ACCOUNT_ID` | OANDA account ID (auto-discovered if omitted) |
| `--env` | `practice` | OANDA environment: `practice\|live` |
| `--log-level` | `info` | Log level |
| `--journal-db` | `./trader.db` | SQLite journal path |

```bash
trader serve --config /etc/trader/trader.yaml
trader serve --token $OANDA_TOKEN --env live --addr :9999
```

---

### `trader api serve`

Start only the REST API server (no live journal). Useful for
backtest-only deployments. Without `--token`, OANDA endpoints return
503; backtest endpoints work unconditionally.

```
trader api serve [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | TCP address to listen on |
| `--token` | `$OANDA_TOKEN` | OANDA API token (enables live order endpoints) |
| `--account-id` | `$OANDA_ACCOUNT_ID` | OANDA account ID |
| `--env` | `practice` | OANDA environment: `practice\|live` |

```bash
trader api serve --addr :8080
trader api serve --token $TOKEN --env practice
```

---

### `trader backtest`

Parent command for backtest operations. Runs `help` when invoked
alone.

#### `trader backtest regress`

Run all YAML configs in a directory (or a single file) and write
JSON + org-mode reports to an output directory. Also rebuilds
`index.org` comparing all runs.

```
trader backtest regress [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `../trading/backtests` | Output directory for reports |
| `--reports` | `reports` | Alternate report directory flag |

Config path defaults to `testdata/configs`; override with the global
`--config` flag.

```bash
trader backtest regress
trader backtest regress --config configs/my-run.yml --out /tmp/reports
```

---

### `trader data`

Download and prepare OHLC candle data. Parent for four subcommands.

#### `trader data download-ticks`

Download missing Dukascopy tick files for the given instruments and month range.

| Flag | Required | Description |
|------|----------|-------------|
| `--instruments` | yes | Comma-separated, e.g. `EURUSD,USDJPY` |
| `--from` | yes | Start month `YYYY-MM` (inclusive) |
| `--to` | yes | End month `YYYY-MM` (inclusive) |

```bash
trader data download-ticks --instruments EURUSD,GBPUSD --from 2024-01 --to 2024-12
```

#### `trader data build-candles`

Build OHLC candles from already-downloaded tick files.

Same flags as `download-ticks`.

```bash
trader data build-candles --instruments EURUSD --from 2024-01 --to 2024-03
```

#### `trader data sync`

Download ticks and build candles in one step.

Same flags as `download-ticks`.

```bash
trader data sync --instruments EURUSD,USDJPY --from 2024-01 --to 2024-12
```

#### `trader data oanda`

Download OHLC candles directly from the OANDA API into the canonical candle store.

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--instrument` | yes | | OANDA-format instrument, e.g. `USD_JPY` |
| `--timeframe` | yes | | Timeframe: `M1`, `H1`, `D` |
| `--from` | yes | | Start date `YYYY-MM-DD` (inclusive) |
| `--to` | yes | | End date `YYYY-MM-DD` (inclusive) |
| `--token` | | `$OANDA_TOKEN` | OANDA API token |
| `--env` | | `practice` | OANDA environment |
| `--raw-dir` | | `/data/raw` | Directory for raw bid+ask candle files |

```bash
trader data oanda --instrument EUR_USD --timeframe H1 --from 2024-01-01 --to 2024-12-31
```

---

### `trader live`

Live trading subsystem.

#### `trader live journal`

Subscribe to the OANDA transaction stream and write closed trades to a journal. Reconnects are handled by `trader serve`; use this subcommand for standalone journaling.

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$OANDA_TOKEN` | OANDA API token |
| `--account-id` | `$OANDA_ACCOUNT_ID` | OANDA account ID |
| `--env` | `practice` | `practice\|live` |
| `--journal` | `csv` | Journal backend: `csv\|sqlite` |
| `--csv-trades` | `live-trades.csv` | Path for CSV trades file |
| `--csv-equity` | `live-equity.csv` | Path for CSV equity file |
| `--sqlite` | `live.db` | Path for SQLite database |
| `--backfill-from` | `0` | If >0, poll transactions from this ID before streaming |

```bash
trader live journal --journal sqlite --sqlite /var/lib/trader/journal.db
trader live journal --journal csv --backfill-from 12345
```

---

### `trader order`

Live order management against an OANDA account.

#### `trader order new`

Size and submit a risk-based market order. Shows a proposal and prompts for confirmation before submitting.

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--instrument` | yes | | OANDA format, e.g. `USD_JPY` |
| `--side` | yes | | `long` or `short` |
| `--risk-pct` | | `1.0` | Percent of account equity to risk |
| `--stop-pips` | | `0` | Stop distance in pips |
| `--token` | | `$OANDA_TOKEN` | OANDA API token |
| `--account-id` | | `$OANDA_ACCOUNT_ID` | OANDA account ID |
| `--env` | | `practice` | `practice\|live` |

```bash
trader order new --instrument EUR_USD --side long --risk-pct 1.0 --stop-pips 20
```

#### `trader order list`

List open trades on the OANDA account.

```bash
trader order list
trader order list --env live
```

#### `trader order close`

Close an open trade fully or partially.

| Flag | Required | Description |
|------|----------|-------------|
| `--trade-id` | yes | Trade ID to close |
| `--units` | | Units to close (default `0` = full close) |

```bash
trader order close --trade-id 12345
trader order close --trade-id 12345 --units 50000
```

#### `trader order transactions`

List OANDA account transactions.

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `0` | Return transactions with ID > this value |
| `--limit` | `25` | Max rows to display (0 = all, most-recent shown) |

```bash
trader order transactions --since 9000 --limit 50
```

#### `trader order transactions-stream`

Subscribe to the OANDA transaction push stream. Prints each incoming transaction until Ctrl-C.

| Flag | Default | Description |
|------|---------|-------------|
| `--heartbeats` | `false` | Also print heartbeat messages |

```bash
trader order transactions-stream
trader order transactions-stream --heartbeats
```

---

### `trader replay`

Replay historical data through the simulation engine.

#### `trader replay pricing`

Replay a CSV tick file through the sim broker. Columns: `time,instrument,bid,ask` (optional extra columns ignored).

| Flag | Default | Description |
|------|---------|-------------|
| `--ticks` | _(required)_ | Path to CSV tick file |
| `--starting-balance` | `100000` | Starting account balance |
| `--account` | `SIM-REPLAY` | Account ID |
| `--close-end` | `false` | Close all open trades at end of replay |
| `--from` | _(none)_ | RFC3339 start time filter |
| `--to` | _(none)_ | RFC3339 end time filter |

```bash
trader replay pricing --ticks ticks.csv --starting-balance 50000
```

#### `trader replay events`

Replay pricing ticks plus scripted order events from a CSV. Columns: `time,instrument,bid,ask,event,p1,p2,p3,p4`.

Same flags as `replay pricing` (`--ticks` is required).

```bash
trader replay events --ticks scripted.csv --close-end
```

---

### `trader mcp serve`

Start the MCP (Model Context Protocol) server on stdio. Claude Code
and Claude Desktop connect to this to use trader as a set of typed
tools.

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$OANDA_TOKEN` | OANDA API token (enables live endpoints) |
| `--account-id` | `$OANDA_ACCOUNT_ID` | OANDA account ID |
| `--env` | `practice` | `practice\|live` |
| `--enable-write` | `false` | Enable `place_order`, `close_trade`, `update_stop` |

```bash
trader mcp serve
trader mcp serve --token $OANDA_TOKEN --enable-write
```

Claude Code / Claude Desktop integration — add to `~/.claude/mcp_servers.json`:

```json
{
  "trader": {
    "command": "trader",
    "args": ["mcp", "serve"]
  }
}
```

---

## REST API

Base URL: `http://localhost:9999` (daemon default) or `http://localhost:8080` (`api serve` default).

All responses are `Content-Type: application/json`. Error responses have shape `{"error": "message"}`. OANDA-dependent endpoints return `503` when no token is configured.

CORS headers (`Access-Control-Allow-Origin: *`) are set on every response.

---

### Health

#### `GET /health` or `GET /api/v1/health`

Always returns 200. Safe for orchestrator liveness probes.

```bash
curl http://localhost:9999/health
```
```json
{"status": "ok"}
```

---

### Account

#### `GET /api/v1/account`

Returns current OANDA account balance, NAV, margin, and open P/L.

Requires OANDA token.

```bash
curl http://localhost:9999/api/v1/account
```
```json
{
  "balance": 10432.51,
  "nav": 10398.12,
  "unrealized_pl": -34.39,
  "margin_used": 340.00,
  "margin_available": 10058.12
}
```

---

### Trades

#### `GET /api/v1/trades`

List all open positions.

Requires OANDA token.

```bash
curl http://localhost:9999/api/v1/trades
```
```json
[
  {
    "id": "1234",
    "instrument": "EUR_USD",
    "units": 10000,
    "entry_price": 1.08512,
    "stop_loss": 1.08312,
    "unrealized_pl": 12.40
  }
]
```

#### `POST /api/v1/trades`

Place a market order. Set `confirm: false` to preview the proposal without submitting.

Requires OANDA token.

Request body:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `instrument` | string | yes | OANDA format, e.g. `EUR_USD` |
| `side` | string | yes | `long` or `short` |
| `risk_pct` | number | | Percent of equity to risk (default `1.0`) |
| `stop_pips` | number | | Stop distance in pips |
| `stop_price` | number | | Explicit stop price (alternative to `stop_pips`) |
| `units` | integer | | Explicit unit size (overrides risk sizing) |
| `confirm` | boolean | | `true` to submit; `false` (default) to preview |

```bash
# Preview
curl -X POST http://localhost:9999/api/v1/trades \
  -H 'Content-Type: application/json' \
  -d '{"instrument":"EUR_USD","side":"long","risk_pct":1.0,"stop_pips":20,"confirm":false}'

# Submit
curl -X POST http://localhost:9999/api/v1/trades \
  -H 'Content-Type: application/json' \
  -d '{"instrument":"EUR_USD","side":"long","risk_pct":1.0,"stop_pips":20,"confirm":true}'
```

Response (`confirm: true`, 201):
```json
{
  "proposal": { "instrument": "EUR_USD", "side": "long", "units": 9200, "entry_price": 1.08512, "stop_price": 1.08312 },
  "filled":   { "order_id": "5001", "trade_id": "1234", "units": 9200, "price": 1.08512 }
}
```

#### `PATCH /api/v1/trades/{id}/stop`

Update stop-loss and/or take-profit on an open trade. Use `0` to leave a level unchanged.

Requires OANDA token.

```bash
curl -X PATCH http://localhost:9999/api/v1/trades/1234/stop \
  -H 'Content-Type: application/json' \
  -d '{"stop_price":1.08400,"take_price":0}'
```
```json
{"trade_id": "1234", "status": "updated"}
```

#### `DELETE /api/v1/trades/{id}`

Close a trade. Add `?units=N` for a partial close; omit for a full close.

Requires OANDA token.

```bash
# Full close
curl -X DELETE http://localhost:9999/api/v1/trades/1234

# Partial close — 5000 units
curl -X DELETE "http://localhost:9999/api/v1/trades/1234?units=5000"
```
```json
{"order_id": "5002", "trade_id": "1234", "units": 9200, "price": 1.08650}
```

---

### Transactions

#### `GET /api/v1/transactions`

Fetch OANDA account transaction history.

Query parameter: `since_id` (integer, optional) — return only transactions with ID > this value.

Requires OANDA token.

```bash
curl "http://localhost:9999/api/v1/transactions?since_id=9000"
```
```json
{
  "transactions": [
    {"id": "9001", "type": "ORDER_FILL", "instrument": "EUR_USD", "units": 9200, "price": 1.08512, "pl": 0, "time": "2024-03-15T10:22:01Z"}
  ],
  "last_transaction_id": 9001
}
```

---

### Backtests

#### `POST /api/v1/backtests/run`

Run one or more YAML backtest configs on the server. No OANDA token required.

Request body:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `config_paths` | array of strings | yes | Server-side file paths or glob patterns |
| `start_date` | string | | ISO-8601 date override for all runs |
| `end_date` | string | | ISO-8601 date override for all runs |

```bash
curl -X POST http://localhost:9999/api/v1/backtests/run \
  -H 'Content-Type: application/json' \
  -d '{"config_paths":["testdata/configs/eurusd-h1-ci45.yml"]}'
```
```json
{
  "count": 1,
  "summaries": [
    {
      "name": "eurusd-h1-2019-2024-adx20-ci45",
      "total_trades": 312,
      "win_rate": 0.52,
      "net_pl": 1842.30,
      "max_drawdown": 0.08
    }
  ]
}
```

---

## SSE Streams

All SSE endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and `X-Accel-Buffering: no`. Connect with `EventSource` in a browser or `curl --no-buffer`.

Each message is formatted as:

```
event: <event-name>
data: <JSON payload>

```

---

### `GET /api/v1/stream/account`

Polls the OANDA account summary every 5 seconds and pushes each snapshot as an `account` event. An initial snapshot is sent immediately on connect.

Requires OANDA token.

```bash
curl --no-buffer http://localhost:9999/api/v1/stream/account
```

```
event: account
data: {"balance":10432.51,"nav":10398.12,"unrealized_pl":-34.39,"margin_used":340.00}

event: account
data: {"balance":10432.51,"nav":10401.87,"unrealized_pl":-30.64,"margin_used":340.00}
```

Browser `EventSource`:

```javascript
const es = new EventSource('http://localhost:9999/api/v1/stream/account');
es.addEventListener('account', e => {
  const snap = JSON.parse(e.data);
  console.log('NAV:', snap.nav);
});
```

---

### `GET /api/v1/stream/events`

Proxies the OANDA live transaction stream. Each trade fill, order cancel, or account event is forwarded as a `transaction` event. OANDA heartbeats are forwarded as `heartbeat` events to prevent proxy timeouts.

Requires OANDA token.

```bash
curl --no-buffer http://localhost:9999/api/v1/stream/events
```

```
event: heartbeat
data: {"time":"2024-03-15T10:22:00Z","lastID":9001}

event: transaction
data: {"id":"9002","type":"ORDER_FILL","instrument":"EUR_USD","units":9200,"price":1.08512,"pl":0,"time":"2024-03-15T10:22:05Z"}
```

Browser `EventSource`:

```javascript
const es = new EventSource('http://localhost:9999/api/v1/stream/events');
es.addEventListener('transaction', e => {
  const tx = JSON.parse(e.data);
  if (tx.type === 'ORDER_FILL') console.log('Fill:', tx);
});
es.addEventListener('heartbeat', e => console.log('heartbeat', JSON.parse(e.data)));
```

---

### `GET /api/v1/stream/backtest/{id}`

Reserved for in-flight backtest progress streaming (issue #113). Currently returns `501 Not Implemented`.

```bash
curl http://localhost:9999/api/v1/stream/backtest/my-run
# HTTP 501 — {"error":"backtest progress streaming not yet implemented (see issue #113)"}
```

---

## MCP Tools

`trader mcp serve` starts a JSON-RPC 2.0 server on stdio using the MCP protocol (`2024-11-05`). Claude Code and Claude Desktop discover available tools via `tools/list` on startup.

Two MCP resources are also exposed (read-only):

| URI | Description |
|-----|-------------|
| `backtest://results` | Lists or reads `.org` report files from the backtest output directory |
| `config://configs` | Lists or reads YAML config files from `testdata/configs/` |

---

### Read-only tools (always available)

#### `get_account_summary`

Return current OANDA account balance, NAV, margin, and unrealized P/L.

No parameters.

```json
{"name": "get_account_summary", "arguments": {}}
```

---

#### `list_open_trades`

Return all open positions on the OANDA account.

No parameters.

```json
{"name": "list_open_trades", "arguments": {}}
```

---

#### `get_transactions`

Return OANDA account transactions with ID greater than `since_id`.

| Parameter | Type | Description |
|-----------|------|-------------|
| `since_id` | integer | Return transactions with ID > this value (0 = from start) |

```json
{"name": "get_transactions", "arguments": {"since_id": 9000}}
```

---

#### `run_backtest`

Run one or more YAML backtest configs and return result summaries. Glob patterns are expanded server-side. No OANDA token required.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config_paths` | array of strings | yes | File paths or glob patterns on the server |

```json
{
  "name": "run_backtest",
  "arguments": {
    "config_paths": ["testdata/configs/eurusd-h1-ci45.yml"]
  }
}
```

---

### Write tools (require `--enable-write`)

These tools are only registered when the server is started with `trader mcp serve --enable-write`.

#### `place_order`

Size and submit a risk-based market order. Set `confirm: false` (the default) to preview without submitting.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `instrument` | string | yes | OANDA instrument, e.g. `EUR_USD` |
| `side` | string | yes | `long` or `short` |
| `stop_pips` | number | yes | Stop distance in pips |
| `risk_pct` | number | | Percent of NAV to risk (default `1.0`) |
| `confirm` | boolean | | `true` to submit; `false` (default) to preview |

```json
{
  "name": "place_order",
  "arguments": {
    "instrument": "EUR_USD",
    "side": "long",
    "stop_pips": 20,
    "risk_pct": 1.0,
    "confirm": false
  }
}
```

---

#### `close_trade`

Close an open trade fully or partially.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `trade_id` | string | yes | OANDA trade ID |
| `units` | integer | | Units to close (0 = full close) |

```json
{"name": "close_trade", "arguments": {"trade_id": "1234", "units": 0}}
```

---

#### `update_stop`

Move the stop-loss and/or take-profit on an open trade. Pass `0` to leave a level unchanged; pass a negative value to cancel it.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `trade_id` | string | yes | OANDA trade ID |
| `stop_price` | number | | New stop-loss price (0 = unchanged, <0 = cancel) |
| `take_price` | number | | New take-profit price (0 = unchanged, <0 = cancel) |

```json
{
  "name": "update_stop",
  "arguments": {
    "trade_id": "1234",
    "stop_price": 1.08400,
    "take_price": 0
  }
}
```

---

## Configuration

### Daemon config file (`trader serve --config`)

Copy `deploy/trader.yaml.example` to `/etc/trader/trader.yaml`.

```yaml
# OANDA credentials
env: practice          # practice | live
token: ""              # leave empty; use OANDA_TOKEN env var instead
account_id: ""         # auto-discovered when blank (errors on multiple accounts)

rest:
  addr: ":9999"        # TCP address for REST API

journal:
  kind: sqlite                              # sqlite | csv
  sqlite_path: /var/lib/trader/journal.db   # when kind=sqlite
  csv_trades: /var/lib/trader/trades.csv    # when kind=csv
  csv_equity: /var/lib/trader/equity.csv    # when kind=csv

data:
  dir: /data/candles   # root directory for candle data

log:
  level: info          # debug | info | warn | error
```

CLI flags override every config file field. Token resolution order: `--token` flag → `OANDA_TOKEN` env var → `~/.config/oanda/pat.txt`.

### Backtest YAML config

Backtest runs are described by YAML files. The `--config` global flag (or the first positional argument for `backtest regress`) points to a single file or a directory of `*.yml` files.

```yaml
version: 1

defaults:
  starting-balance: 10000   # account capital
  account-ccy: USD
  scale: 100000             # price scaling (PriceScale)
  risk-pct: 1.0             # percent of equity risked per trade
  source: oanda             # candle data source

runs:
  - name: eurusd-h1-ema-adx
    data:
      instrument: EURUSD    # instrument symbol
      timeframe: H1         # M1 | H1 | D
      from: 2019-01-01      # start date (inclusive)
      to:   2024-12-31      # end date (inclusive)
    strategy:
      kind: ema-cross-adx   # registered strategy name
      params:
        fast: 9
        slow: 21
        adx_period: 14
        adx_threshold: 20.0
        min_spread: 0.0003
    exit:
      kind: chandelier       # exit strategy (optional)
      params:
        atr_period: 14
        multiplier: 3.0
    regime:
      kind: choppiness       # regime filter (optional)
      params:
        period: 14
        threshold: 45.0
```

Available built-in strategy names: `ema-cross`, `ema-cross-adx`, `donchian`, `noop`, `fake`, `lifecycle`, `tmpl`.

### Environment variables

| Variable | Description |
|----------|-------------|
| `OANDA_TOKEN` | OANDA personal access token |
| `OANDA_ACCOUNT_ID` | OANDA account ID (skip auto-discovery) |
| `TRADER_RUN_DUKASCOPY_TESTS` | Set to `1` to enable network-hitting Dukascopy download tests |
