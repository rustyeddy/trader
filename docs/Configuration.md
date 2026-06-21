# Configuration

Trader has several configuration scopes. They are intentionally separate
because runtime settings, deterministic backtests, managed live portfolios,
and the long-running daemon have different lifecycles.

| Scope | Primary types | Used by |
|---|---|---|
| Global runtime | `GlobalConfig`, `RootConfig` | All CLI commands |
| Backtest | `Config`, `RunDefaults`, `RunConfig` | `trader backtest run` |
| Live portfolio | `service.PortfolioConfig` | `trader bot start --config` |
| Daemon | `cmd/serve.DaemonConfig` | `trader serve --config` |

Do not combine these schemas into one YAML file. In particular, a backtest
file is not a global runtime file.

For the complete command-line reference, see
[trader-cli.md](trader-cli.md). This document describes configuration files,
defaults, and precedence rather than every command-specific flag.

## Global runtime configuration

Global configuration controls logging, the candle store, replay journal
output, and default OANDA credentials.

### Search and merge order

At command startup, `LoadGlobalConfig` merges files in this order:

1. `/etc/trader/*.yml`
2. `~/.config/trader/*.yml`
3. The explicit root `--config` path, when supplied
4. Explicit CLI flags

Files inside each directory are processed alphabetically. Later non-empty
values replace earlier values. Missing standard directories are ignored, but
an unreadable or invalid file returns an error.

Only `*.yml` files are discovered automatically. An explicitly supplied file
is parsed as YAML regardless of its extension.

CLI flags win only when the user explicitly set the flag. Otherwise a
non-empty global YAML value replaces the flag's built-in default.

### Global YAML schema

```yaml
oanda:
  account_id: ""
  env: practice
  token: ""

log:
  level: info
  format: text
  file: ""

data:
  dir: /srv/trading/data/candles

db: ./trader-journal
```

| Key | Meaning |
|---|---|
| `oanda.token` | OANDA personal access token; prefer an environment variable or token file |
| `oanda.account_id` | Default account; may be auto-discovered when the token has one account |
| `oanda.env` | `practice` or `live` |
| `log.level` | `debug`, `info`, `warn`, or `error` |
| `log.format` | `text` or `json` |
| `log.file` | Optional log file |
| `data.dir` | Canonical candle-store root |
| `db` | Replay journal output base path |

See [config.yml.example](../config.yml.example) for a copyable user-level
configuration.

### Root CLI defaults

`cmd/main.go` creates one `RootConfig` shared by the command adapters.

| Flag | Default | Purpose |
|---|---|---|
| `--config` | empty | Explicit configuration path; interpretation can be command-specific |
| `--db` | `./trader-journal` | Replay journal output base |
| `--report` | empty | Backtest/report path used by applicable commands |
| `--data-dir` | `/srv/trading/data/candles` | Canonical candle-store root |
| `--log-level` | `debug` | Root logging level |
| `--log-format` | `text` | Root logging format |
| `--log-file` | `./trader.log` | Log file; an empty value enables stdout-only root logging |
| `--no-color` | `false` | Disable colored output |

`RootConfig.GlobalPath` exists but is not populated by a CLI flag or global
YAML key.

### The `--config` exception for backtests

`trader backtest run --config PATH` uses `PATH` as a backtest file,
directory, or glob. It is deliberately not loaded as global runtime YAML.

Backtest configuration path precedence is:

1. Positional `trader backtest run PATH`
2. Local `trader backtest run --config PATH`
3. The inherited root config path
4. `$TRADER_BACKTEST_DIR/configs`
5. `/srv/trading/backtests/configs` when the environment variable is unset

The report output directory is selected by `--out`, then
`$TRADER_BACKTEST_DIR/reports`, then `/srv/trading/backtests/reports`.

## OANDA credentials and precedence

Avoid storing tokens in committed YAML. Commands that talk to OANDA generally
resolve credentials from:

1. A command-specific flag
2. Merged global configuration
3. `OANDA_TOKEN` or `OANDA_ACCOUNT_ID`
4. `~/.config/oanda/pat.txt` for the token

The precise first two steps vary slightly by command because flags belong to
the edge adapter. `trader serve`, `trader live journal`, MCP, data download,
and bot-local mode each perform their own final resolution.

If no account ID is supplied and a token has access to exactly one account,
the service can discover it. Multiple accounts require an explicit
`--account-id` or configured `account_id`.

Never log or commit an OANDA token. Use the practice environment unless live
trading is explicitly intended.

## Backtest configuration

Backtest files are YAML, YML, or JSON. They contain shared defaults and one or
more named runs.

```yaml
version: 1

defaults:
  starting-balance: 10000
  account-ccy: USD
  scale: 100000
  risk-pct: 1.0
  stop-pips: 20
  take-pips: 40
  slippage-pips: 0.2
  max-spread-pips: 3.0
  source: oanda

runs:
  - name: eurusd-h1-2024-ema-cross
    data:
      source: oanda
      instrument: EURUSD
      timeframe: H1
      from: 2024-01-01
      to: 2025-01-01
      strict: true
    strategy:
      kind: ema-cross
      params:
        fast: 9
        slow: 21
        atr_period: 14
        atr_multiplier: 1.5
    exit:
      kind: chandelier
      params:
        atr_period: 14
        multiplier: 2.0
    regime:
      kind: composite
      filters:
        - kind: session
          params:
            start_hour: 7
            end_hour: 17
        - kind: adx-d1
          params:
            period: 14
            threshold: 20
```

### Top-level fields

| Field | Required | Meaning |
|---|---|---|
| `version` | No | Defaults to `1` when omitted |
| `defaults` | No | Values shared across runs |
| `runs` | Yes | At least one run is required |

### Execution-affecting defaults

| Field | Meaning |
|---|---|
| `starting-balance` | Initial account balance |
| `risk-pct` | Percent of equity risked per trade; `1.0` means 1% |
| `stop-pips` | Fallback stop distance |
| `take-pips` | Fallback take-profit distance |
| `slippage-pips` | Adverse slippage applied to opens and closes |
| `max-spread-pips` | Suppress opens when the candle spread is larger |
| `source` | Default candle source when `runs[].data.source` is empty |

The schema also currently accepts `account-ccy`, `scale`, `strict`, `rr`, and
`units` in `defaults`. These fields are parsed but are not applied by the
current backtest compiler. Do not rely on them to change execution behavior.

### Run data fields

| Field | Required | Meaning |
|---|---|---|
| `name` | Yes in practice | Report name and filename prefix |
| `data.instrument` | Yes | Normalized internal symbol such as `EURUSD` |
| `data.timeframe` | Yes | `M1`, `H1`, `H4`, or `D1` where supported |
| `data.from` | Yes | Inclusive UTC date, `YYYY-MM-DD` |
| `data.to` | Yes | Exclusive UTC date, `YYYY-MM-DD` |
| `data.source` | No | Overrides `defaults.source`; defaults ultimately to `candles` |
| `data.strict` | No | Parsed per-run strictness override |

The time range is half-open: `[from, to)`. To include all of 2024, use
`from: 2024-01-01` and `to: 2025-01-01`.

The current compiler parses `data.strict`, but does not copy it into the
execution candle request. Treat that field as non-operative until the
implementation is completed.

### Strategy, exit, and regime sections

`strategy.kind` selects a registered constructor. `strategy.params` is an
untyped map owned and validated by that strategy. Consult its implementation
and examples under `testdata/configs/`; parameter names are not globally
standardized.

An empty `exit` selects `NoopExit`. The implemented non-noop exit is:

```yaml
exit:
  kind: chandelier
  params:
    atr_period: 14
    multiplier: 2.0
```

An empty `regime` selects `NoopRegime`. Registered regime kinds currently
include `choppiness`, `choppiness-d1`, `session`, `adx-d1`, `weekly-ema`,
`atr-percentile`, and `composite`. Composite filters use an AND relationship.

Configuration parameters enter as ordinary YAML numbers and strings, then
must be validated and converted to fixed-point values during compilation or
strategy construction.

### Reports and configuration hashes

Backtest reports use `<run-name>-<config-hash>.json` and
`<run-name>-<config-hash>.org`. The hash includes the run's data, strategy,
exit, regime, and execution-affecting defaults. The run name is deliberately
excluded from the hash.

## Live portfolio configuration

Portfolio YAML is consumed by `service.LoadPortfolioConfig`, primarily via
`trader bot start --config FILE`.

```yaml
env: practice
account_id: ""
risk_pct: 1.0
drawdown_circuit_pct: 10.0
local_warmup_bars: 500

instruments:
  - instrument: EUR_USD
    timeframe: H1
    tick_interval: 60s
    risk_pct: 0.5
    max_units: 10000
    warmup_bars: 100
    local_warmup_bars: 500

    strategy:
      kind: donchian
      params:
        period: 20

    exit:
      kind: chandelier
      params:
        atr_period: 14
        multiplier: 2.0

    regime:
      kind: composite
      filters:
        - kind: session
          params:
            start_hour: 7
            end_hour: 17
```

Portfolio defaults are `env: practice`, `risk_pct: 1.0`, and
`drawdown_circuit_pct: 10.0`. A per-instrument `risk_pct` overrides the
portfolio value. A non-positive per-instrument value falls back to the
portfolio default.

`local_warmup_bars` can be set globally and overridden per instrument.
`warmup_bars` defaults to 100 for candle-adapted strategies. Native live
strategies, such as `pulse`, bypass the candle adapter.

Portfolio instruments use OANDA wire names such as `EUR_USD`. Backtest and
store configuration normally use normalized names such as `EURUSD`.

## Daemon configuration

`trader serve --config FILE` reads a daemon-specific YAML file. Command flags
override applicable file values.

```yaml
env: practice
token: ""
account_id: ""

rest:
  addr: ":9999"

journal:
  kind: json
  tradespath: ./live-trades.jsonl
  equitypath: ./live-equity.jsonl

data:
  dir: /srv/trading/data/candles

log:
  level: info
  format: text
  file: ""
```

Daemon defaults are:

| Field | Default |
|---|---|
| `env` | `practice` |
| `rest.addr` | `:9999` |
| `journal.kind` | `json` |
| `journal.tradespath` | `./live-trades.jsonl` |
| `journal.equitypath` | `./live-equity.jsonl` |
| `log.level` | `info` |

The current `JournalConfig` fields have no explicit YAML tags, so daemon YAML
must use `tradespath` and `equitypath`. The older
`deploy/trader.yaml.example` spelling `trades_path` and `equity_path` does not
populate those fields. The `--journal-trades` and `--journal-equity` flags
avoid that ambiguity.

`postgres` is recognized as a journal kind but is not implemented. Use `json`
or `csv`.

The daemon can start its REST API, embedded UI, and read-only MCP endpoint
without an OANDA token. Live journal and broker-backed capabilities remain
disabled. `--mcp-enable-write` enables unauthenticated write tools on the
HTTP MCP endpoint; do not expose that mode to an untrusted network.

## Legacy private app configuration

`app_config.go` still contains the private `appConfig`, `defaultConfig`, and
`loadFromFile` implementation for an older simulation configuration. These
identifiers are unexported and are not the supported public configuration
API. New commands and documentation must use the global, backtest, portfolio,
or daemon schemas described above.

## Store configuration in tests and embedded callers

The default store root is `/srv/trading/data/candles`. CLI startup calls
`SetDataDir` after resolving global configuration.

Tests should use `NewStoreAt(t.TempDir())` and `SwapStore`, restoring the
returned function afterward. Tests must not read or modify the operational
`/srv/trading/data` tree.

## Troubleshooting

### A global setting appears ignored

Check that the file ends in `.yml` when relying on automatic discovery, that a
later alphabetically sorted file does not override it, and that an explicit
CLI flag was not supplied.

### A backtest file is parsed as global configuration

Use the local form:

```bash
trader backtest run --config path/to/backtest.yml
```

or pass the file positionally:

```bash
trader backtest run path/to/backtest.yml
```

### Candle data cannot be found

Verify `--data-dir` or `data.dir`. The value must point at the canonical
`candles` root, not its parent `/srv/trading/data` and not the raw-data tree.

### OANDA account discovery fails

Set an explicit `account_id` or `--account-id` when the token can access
multiple accounts. Verify that `env` matches the token's practice/live
environment.
