# Trader Service and API Reference

This document is a map of the current application surfaces. It intentionally
avoids duplicating every flag and JSON field:

- Generate exact CLI syntax with `trader docs`, or use
  [trader-cli.md](trader-cli.md).
- See [Configuration.md](Configuration.md) for configuration schemas and
  precedence.
- See [architecture.org](architecture.org) for dependency and execution flow.
- See [oanda.md](oanda.md) for broker-client behavior and safety.

## Layering

`service.Service` is the protocol-independent application layer. CLI, REST,
and MCP adapters validate transport input, call service methods, and map
results/errors at the edge.

```text
cmd/              api/rest/              api/mcp/
   \                   |                    /
                    service/
                       |
          trader domain + brokers/oanda
```

Transport packages must not own trading, sizing, replay, data, journal, or bot
policy.

## Service construction

```go
svc, err := service.New(service.Config{
    Env:       "practice",
    Token:     token,
    AccountID: accountID,
    Log:       logger,
})
```

`service.New` creates an OANDA-backed service and requires a token, with a
fallback to `~/.config/oanda/pat.txt`. Backtest and local-data use cases can
instead use `&service.Service{Log: logger}` without OANDA.

`ResolveAccount` keeps an explicit account ID or discovers the sole account
available to a token. It returns `AmbiguousAccountError` when the user must
choose among several accounts.

Only the OANDA practice environment is currently enabled.

## Service capabilities

### Account, prices, and orders

| Method | Purpose |
|---|---|
| `GetAccountSummary` | Current balance, NAV, margin, and P/L |
| `GetPrices` | Current bid/ask for requested instruments |
| `GetTransactions` | Poll transactions after an ID |
| `StreamTransactions` | Transaction/heartbeat stream |
| `ListOpenTrades` | Current broker positions |
| `PlaceMarketOrder` | Validate, size, optionally cap, preview, or place an order |
| `CloseTrade` | Full or partial close |
| `UpdateTradeStop` | Replace/cancel stop-loss or take-profit |

These methods require an OANDA client and resolved account where applicable.
Order methods are external side effects.

### Backtests and reports

| Method/function | Purpose |
|---|---|
| `RunBacktest` | Execute one compiled run |
| `RunBacktestConfigs` | Load and execute config files |
| `RunBacktestPathSpecs` | Expand files, directories, and globs |
| `RunBacktestConfigsAndWriteReports` | Execute and write JSON/Org reports |
| `WriteBacktestReports` | Persist summaries and rebuild the Org index |
| `ListBacktestSummaries` | Read saved JSON summaries |
| `ReadBacktestSummaryByName` | Read one saved summary |
| `ListBacktestOrgReports` | List saved Org reports |
| `ReadBacktestOrgReport` | Read one Org report |

Backtest-only operations do not require OANDA.

### Candle data and analysis

| Method | Purpose |
|---|---|
| `CandlesCSV` | Read canonical local candles as scaled CSV |
| `DataStats` | Dataset spread/trend/swing/session statistics |
| `ValidateCandleData` | Check stored completeness and raw-source consistency |
| `DownloadOandaCandles` | Download raw OANDA bid/ask data and derive canonical candles |
| `UpdateOandaCandles` | Incrementally fill stored OANDA data |
| `DeriveCanonicalFromRaw` | Convert a raw source file into canonical storage |
| `PipValues` | Pip-value table for instruments/units |
| `PositionCalc` | Position, notional, margin, and pip calculations |

Reads can operate locally. Download/update operations require OANDA and write
the configured store.

### Replay and review

`RunReplay` executes a registered strategy against stored candles and returns
bars plus signal events. `ParseReviewCSV` parses uploaded review CSV rows into
typed review records.

### Live runners, portfolios, bots, and journals

| Method | Purpose |
|---|---|
| `RunLiveStrategy` | Poll OANDA and run one `LiveStrategy` |
| `RunPortfolio` | Run instrument strategies concurrently with shared drawdown gating |
| `StartBot` | Build and launch a managed live bot |
| `StopBot` / `StopAllBots` | Cancel and wait for managed bots |
| `ListBots` / `GetBot` | Bot status snapshots |
| `OpenJournal` | Open CSV or JSONL journals |
| `RunLiveJournal` | Backfill and stream OANDA transactions into a journal |

Managed bots live in the `trader serve` process. Context cancellation owns
runner lifetime. Broker positions remain external state and may outlive a
process.

## CLI

Current top-level commands:

| Command | Surface |
|---|---|
| `trader account` | OANDA account inspection |
| `trader backtest` | Run configs and browse/regress saved results |
| `trader bot` | Start, stop, list, inspect, and report live bots |
| `trader data` | Download, update, validate, inspect, and build candles |
| `trader docs` | Generate CLI reference documentation |
| `trader health` | Query a running daemon |
| `trader live journal` | Standalone OANDA transaction journal |
| `trader mcp` | MCP server over stdio |
| `trader order` | Prices and live order/trade management |
| `trader replay` | Pricing or scripted-event replay |
| `trader review` | Parse a forex review CSV |
| `trader serve` | REST, SSE, UI, HTTP MCP, bots, and live journal daemon |
| `trader version` | Build version |

Notable subcommands:

```text
backtest: candles configs get list org regress run
bot:      get list report start stop
data:     build-candles candles download-ticks oanda pip-value position
          stats sync update validate-candles
order:    close list new prices transactions transactions-stream update-stop
replay:   events pricing
```

There is no `trader api serve` command and no `trader live run` or
`trader live portfolio` command in the current command tree. Managed live
strategies use `trader bot` and the `trader serve` process.

Global defaults include:

| Flag | Default |
|---|---|
| `--data-dir` | `/srv/trading/data/candles` |
| `--db` | `./trader-journal` |
| `--log-file` | `./trader.log` |
| `--log-format` | `text` |
| `--log-level` | `debug` |

Use `trader <command> --help` for authoritative flags.

## REST API

`trader serve` exposes the following routes. OANDA-backed routes return an
error when the daemon has no configured broker client; local data, replay,
backtest, health, and saved-report routes can operate without one.

### Health and version

| Method | Path |
|---|---|
| GET | `/health` |
| GET | `/api/v1/health` |
| GET | `/api/v1/version` |

### Account and trading

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/account` | Account summary |
| GET | `/api/v1/prices` | Current prices |
| GET | `/api/v1/trades` | Open trades |
| POST | `/api/v1/trades` | Preview or place an order |
| PATCH | `/api/v1/trades/{id}/stop` | Update stop/take |
| DELETE | `/api/v1/trades/{id}` | Full or partial close |
| GET | `/api/v1/transactions` | Transaction history |

### Candles and calculations

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/candles/validate` | Store validation |
| GET | `/api/v1/candles/{instrument}` | Canonical candle CSV |
| GET | `/api/v1/candles/{instrument}/stats` | Dataset statistics |
| GET | `/api/v1/pip-values` | Pip values |
| GET | `/api/v1/position` | Position/notional calculation |

### Backtests, replay, and review

| Method | Path |
|---|---|
| POST | `/api/v1/backtests/run` |
| POST | `/api/v1/backtests/regress` |
| GET | `/api/v1/backtests/configs` |
| GET | `/api/v1/backtests` |
| GET | `/api/v1/backtests/{name}` |
| GET | `/api/v1/backtests/{name}/org` |
| GET | `/api/v1/backtests/{name}/candles` |
| POST | `/api/v1/replay` |
| POST | `/api/v1/review` |

### Bots

| Method | Path |
|---|---|
| POST | `/api/v1/bots` |
| GET | `/api/v1/bots` |
| GET | `/api/v1/bots/{id}` |
| DELETE | `/api/v1/bots/{id}` |

### SSE

| Method | Path | Status |
|---|---|---|
| GET | `/api/v1/stream/account` | Polls and emits account snapshots |
| GET | `/api/v1/stream/events` | Proxies transaction events and heartbeats |
| GET | `/api/v1/stream/backtest/{id}` | Reserved; currently `501 Not Implemented` |

SSE clients must handle disconnect/reconnect. Broker streams require OANDA.

### HTTP MCP and UI

When configured by `trader serve`, `POST /mcp` exposes MCP JSON-RPC and `/`
serves the embedded Svelte UI. The HTTP MCP endpoint and REST server currently
allow broad CORS and have no built-in authentication. Do not enable MCP writes
or expose broker routes to an untrusted network without a protected reverse
proxy or equivalent control.

## MCP

`trader mcp` serves line-delimited JSON-RPC over stdin/stdout. `trader serve`
can mount the same protocol at `POST /mcp`.

### Tools

| Read/local or broker tool | Purpose |
|---|---|
| `get_account_summary` | OANDA account summary |
| `list_open_trades` | OANDA positions |
| `get_transactions` | OANDA transactions |
| `get_prices` | Current OANDA bid/ask |
| `run_backtest` | Run config path specs and write reports |
| `get_candles_csv` | Local scaled candle CSV |
| `get_candle_stats` | Local dataset statistics |
| `validate_candles` | Local store validation |
| `get_pip_values` | Pip-value calculation |
| `get_position` | Position/notional calculation |
| `get_version` | Build version |
| `get_health` | Process health |
| `list_bots` | Managed bot snapshots |
| `get_bot` | One managed bot |

Write-capable tools are registered only with `--enable-write`:

| Tool | Side effect |
|---|---|
| `start_bot` | Starts a managed live runner |
| `stop_bot` | Stops a managed runner |
| `download_candles` | Downloads and writes OANDA candles |
| `place_order` | Places a broker order |
| `close_trade` | Closes broker exposure |
| `update_stop` | Changes broker stop/take orders |

Some read tools still require OANDA; local backtest/candle/calculation tools do
not. A token does not imply write permission.

### Resources

| URI | Purpose |
|---|---|
| `backtest://results` | List/read saved Org backtest reports |
| `config://configs` | List/read server-side backtest YAML files |

Resource paths are constrained to their configured roots.

## Journals and reports

Live journals support CSV and JSONL. SQLite support described by older
versions of this document was removed.

Backtest reports are JSON and Org files named
`<run-name>-<config-hash>`, plus an Org index. CLI, REST, and MCP share the
same service report functions.

## Error handling

Service methods return contextual Go errors. CLI returns them to Cobra, REST
maps them to status/JSON at the edge, and MCP maps them to JSON-RPC/tool
content. Server errors must not expose tokens, authorization headers, or
sensitive internal paths.

Long-running and network operations accept `context.Context`; cancellation is
the normal shutdown path.

## Verification

Transport tests do not require listening sockets:

- service methods use injected clients/executors and temporary stores;
- REST uses `httptest`;
- MCP exercises JSON-RPC framing directly;
- live and network-hitting tests are opt-in.

Run:

```bash
make vet
make test
make blackbox
```

Use `make sweep` after strategy changes.
