# Trader User's Guide

This guide enumerates the actual commands and flags `trader` (the CLI in
`cmd/trader`) currently supports, verified directly against `--help` output
from a real build — not aspirational. Trader is under active development
([Project Status](../README.md#project-status)); commands and flags will
change as milestones land. See the
[Developer's Guide](DevelopersGuide.md) for the packages behind each
command.

## Building

```sh
go build -o trader ./cmd/trader
```

## Global Flags

Every command accepts these, inherited from the root command:

| Flag | Default | Meaning |
|---|---|---|
| `--log-format` | `text` | `text` or `json` |
| `--log-level` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR` |
| `--log-output` | `stderr` | `stderr`, `stdout`, or a file path |

## Command Overview

```
trader
├── data        historical market-data commands
│   ├── bars        read canonical historical bars
│   ├── build       build and publish canonical data from raw data
│   ├── coverage    report canonical/raw coverage and gaps
│   ├── plan        report the work required to make a dataset available
│   ├── sync        acquire raw data required to make a dataset available
│   └── update      plan, sync, and build a dataset in one step
├── broker      simulated broker account inspection and order submission
│   ├── accounts    list the simulated broker's accounts
│   ├── snapshot    show the simulated account's current snapshot
│   └── submit      submit an order to the simulated broker
├── execution   execution/risk pipeline inspection and order submission
│   ├── evaluate    size, plan, and risk-evaluate an intent (no submission)
│   └── submit      size, plan, risk-evaluate, and submit an intent
└── backtest    run backtests and inspect their results
    ├── run         run a backtest and render/persist its result
    └── show        render a previously run backtest's persisted result
```

`trader completion` also exists (standard Cobra shell-completion
boilerplate) and isn't covered further here.

---

## `trader data` — Historical Market Data

`marketdata.Manager`'s CLI surface: query canonical bars, inspect coverage,
and plan/sync/build/update a dataset. Every `data` subcommand takes the same
two positional arguments and shares the same flag set.

**Usage:** `trader data <subcommand> INSTRUMENT INTERVAL --from ... --to ...`

INSTRUMENT is a plain symbol (e.g. `EURUSD`); INTERVAL is one of the values
listed under `backtest run` below.

### Shared `data` flags

| Flag | Default | Meaning |
|---|---|---|
| `--from` | — | range start (`YYYY-MM-DD` or RFC3339), **required** |
| `--to` | — | range end (`YYYY-MM-DD` or RFC3339), **required** |
| `--format` | `table` | `table` or `json` |
| `--provider` | `oanda` | canonical dataset provider name |
| `--raw-root` | `$XDG_DATA_HOME/trader/raw/<provider>` | raw provider archive root |
| `--store-root` | `$XDG_DATA_HOME/trader/data` | canonical data store root |
| `--oanda-base-url` | — | OANDA API base URL; required only for `sync`/`update` |

The OANDA API token itself is never a flag — set the `TRADER_OANDA_TOKEN`
environment variable instead.

### `trader data bars INSTRUMENT INTERVAL`

Reads canonical historical bars for the given range.

```sh
trader data bars EURUSD H1 --from 2024-01-01 --to 2024-02-01
```

### `trader data plan INSTRUMENT INTERVAL`

Reports what work (if any) is required to make the requested dataset
available — read-only, performs no acquisition or building.

### `trader data sync INSTRUMENT INTERVAL`

Acquires raw provider data for the requested range. Requires
`--oanda-base-url` and `TRADER_OANDA_TOKEN`.

### `trader data build INSTRUMENT INTERVAL`

Builds and publishes canonical data from raw data already present under
`--raw-root` — never fetches from a live provider.

### `trader data update INSTRUMENT INTERVAL`

Runs plan, then sync, then build, as each step actually requires — the
one-command path to "make sure this dataset is current."

### `trader data coverage INSTRUMENT INTERVAL`

Reports canonical/raw coverage and any gaps for the dataset over the given
range.

---

## `trader broker` — Simulated Broker

Inspect and submit orders against a fresh, in-memory simulated broker.
**Every invocation builds a new simulator; nothing persists between
separate `trader` invocations.**

### Shared `broker` flags

| Flag | Default | Meaning |
|---|---|---|
| `--account-id` | freshly generated | account id to use |
| `--currency` | `USD` | account currency |
| `--starting-cash` | `10000` | starting account cash amount |

### `trader broker accounts`

Lists the simulated broker's accounts. `--format table\|json`.

### `trader broker snapshot`

Shows the simulated account's current snapshot (equity, positions, open
orders). `--format table\|json`.

### `trader broker submit`

Submits one order directly to the simulated broker (no sizing or risk
evaluation — see `trader execution submit` for that).

| Flag | Default | Meaning |
|---|---|---|
| `--symbol` | — | instrument symbol, e.g. `EURUSD`, **required** |
| `--side` | — | `buy` or `sell`, **required** |
| `--quantity` | — | order quantity, **required** |
| `--type` | `market` | `market`, `limit`, `stop`, or `stop-limit` |
| `--price` | — | fill price, required for `--type market` |
| `--limit-price` | — | required for `--type limit` or `stop-limit` |
| `--stop-price` | — | required for `--type stop` or `stop-limit` |
| `--tif` | `GTC` | time in force: `GTC`, `DAY`, `IOC`, or `FOK` |
| `--tick-size` | `0.00001` | simulator tick size |
| `--quantity-increment` | `1` | simulator quantity increment |
| `--multiplier` | `1` | simulator contract multiplier |
| `--format` | `table` | `table` or `json` |

```sh
trader broker submit --symbol EURUSD --side buy --quantity 1000 \
  --type market --price 1.10050
```

---

## `trader execution` — Execution/Risk Pipeline

Drives one `order.Intent` through the real sizing → planning → risk →
(optionally) submission pipeline (`pipeline.Pipeline`) against a fresh
simulated broker — the same path a backtest or a future live runtime uses.
Shares `broker`'s `--account-id`/`--currency`/`--starting-cash` flags.

### `trader execution evaluate`

Sizes, plans, and risk-evaluates an intent **without** submitting it.

| Flag | Default | Meaning |
|---|---|---|
| `--symbol` | — | instrument symbol, **required** |
| `--side` | — | `buy` or `sell`, **required** |
| `--adverse-distance` | — | adverse price distance used for sizing, **required** |
| `--risk-fraction` | `0.01` | fraction of account equity to risk (1%) |
| `--reference-price` | — | valuation price for value-based risk rules (optional) |
| `--tick-size` | `0.00001` | simulator tick size |
| `--quantity-increment` | `1` | simulator quantity increment |
| `--multiplier` | `1` | simulator contract multiplier |
| `--format` | `table` | `table` or `json` |

### `trader execution submit`

Same as `evaluate`, plus an actual submission if risk approves. Adds:

| Flag | Default | Meaning |
|---|---|---|
| `--price` | — | fill price for the resulting market order, **required** |

```sh
trader execution submit --symbol EURUSD --side buy \
  --adverse-distance 0.0050 --risk-fraction 0.01 --price 1.10050
```

---

## `trader backtest` — Backtesting

### `trader backtest run`

Runs a backtest over the M5 application service (`service/backtest`) and
persists/renders its result. Canonical market data must already exist
under `--data-store-root`/`--data-raw-root` (via `trader data build` /
`trader data sync`) — `run` never syncs from a live provider itself.

There are two strategy paths:

- **Without `--config`:** a provisional demo strategy — one buy-and-hold
  entry per instrument's first bar. `--symbol` may be repeated for a
  multi-instrument run (one shared account/pipeline, not a per-symbol
  engine).
- **With `--config`:** the real `strategy/emacross` EMA-crossover strategy,
  for exactly one instrument, configured from a YAML file (see below). Any
  explicit flag still overrides its corresponding config-file value.

| Flag | Default | Meaning |
|---|---|---|
| `--symbol` | — | instrument symbol, repeatable, **required** (or via `--config`) |
| `--interval` | `H1` | `M1`, `H1`, `H4`, `D1`, or `W1` |
| `--from` | — | replay range start, **required** (or via `--config`) |
| `--to` | — | replay range end, **required** (or via `--config`) |
| `--currency` | `USD` | account currency |
| `--starting-cash` | `10000` | starting account cash amount |
| `--risk-fraction` | `0.01` | fraction of account equity to risk |
| `--adverse-distance` | — | adverse price distance for sizing, **required** (or via `--config`) |
| `--warmup-bars` | `0` | warm-up bars before the **demo** strategy may trade (ignored with `--config`) |
| `--data-raw-root` | — | raw archive root, **required** |
| `--data-store-root` | `/srv/trading/data/canonical`\* | canonical data store root; an explicit empty value opts into a fresh temp dir per run |
| `--provider` | `oanda` | market data provider name |
| `--config` | — | YAML file supplying backtest/strategy parameters (see below) |
| `--strategy-name` | — | must equal `ema-cross` when `--config` is used |
| `--fast-period` | — | EMA fast period (only with `--config`) |
| `--slow-period` | — | EMA slow period (only with `--config`) |
| `--allowed-side` | `both` | restrict the EMA strategy: `both`, `long-only`, or `short-only` (only with `--config`) |
| `--journal` | — | optional path to write a durable JSONL audit trail; path must not already exist |
| `--output-dir` | `./backtest-runs` | where run snapshots are written / `show` reads from |
| `--format` | `table` | `table`, `json`, or `org` |

\* The `/srv/trading/data/canonical` default is this repository's own local
operational choice (issue #268) — a fresh clone on another machine should
pass `--data-store-root` explicitly, or rely on the automatic temporary-
directory fallback by passing an explicit empty value.

```sh
trader backtest run \
  --symbol EURUSD --interval H1 --from 2024-01-01 --to 2024-06-01 \
  --starting-cash 10000 --risk-fraction 0.01 --adverse-distance 0.0050 \
  --data-raw-root /path/to/raw/oanda --format table
```

#### `--config` YAML reference

```yaml
backtest:
  symbol: EURUSD              # required
  interval: H1                # default H1
  from: 2015-01-01T00:00:00Z  # required
  to: 2025-01-01T00:00:00Z    # required
  currency: USD                # default USD
  starting_capital: 10000      # default 10000
  risk_fraction: 0.01          # default 0.01
  adverse_distance: 0.0050     # required
  data_store_root: /path/to/canonical  # default /srv/trading/data/canonical

strategy:
  name: ema-cross              # default ema-cross; no other value is supported
  fast_period: 20              # default 20
  slow_period: 50              # default 50
  allowed_side: both           # default both; or long-only / short-only
```

Precedence for every field is: explicit CLI flag > `--config` file value >
`TRADER_BACKTEST_*`/`TRADER_STRATEGY_*` environment variable > the default
shown above.

### `trader backtest show <run-id>`

Renders a persisted run snapshot written by a prior `run` — no
recomputation, byte-identical to what `run` itself rendered.

| Flag | Default | Meaning |
|---|---|---|
| `--output-dir` | `./backtest-runs` | directory the run's snapshot was written to |
| `--format` | `table` | `table`, `json`, or `org` |

```sh
trader backtest show run_01HKK5WY00D5982ACAHT01Q80K --format org
```

---

## Environment Variables

Every flag documented above is also settable as an environment variable,
following `config`'s naming convention: prefix `TRADER_`, then the
dotted config path, uppercased with `.` replaced by `_`. For example,
`backtest.risk_fraction` becomes `TRADER_BACKTEST_RISK_FRACTION`. See the
[`config` package doc comment](../config/doc.go) for the full rule.

One credential is environment-only and has no flag at all:

| Variable | Meaning |
|---|---|
| `TRADER_OANDA_TOKEN` | OANDA API token, required for `trader data sync`/`update` |
