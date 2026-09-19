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

| Flag              | Default  | Meaning                             |
|-------------------|----------|--------------------------------------|
| `--log-format`    | `text`   | `text` or `json`                    |
| `--log-level`     | `INFO`   | `DEBUG`, `INFO`, `WARN`, or `ERROR` |
| `--log-output`    | `stderr` | `stderr`, `stdout`, or a file path  |
| `--version`, `-v` | —        | print the version and exit (see [Versioning](#versioning) below) |

## Versioning

Trader's version is derived automatically from git tags at build time
(ADR-064) — no source file is bumped by hand before a release.

`trader --version`/`trader -v` prints the compact form, e.g. `trader
version v0.3.0` for a build made exactly at tag `v0.3.0`, or `trader
version v0.3.0-12-gabc1234-dirty` for a development build 12 commits
past that tag with uncommitted local changes.

`trader version` prints a fuller, multi-line report:

```
trader v0.3.0
commit: abc1234def012
built: 2026-09-15T18:00:00Z
```

Both forms require a build made via `make build`/`make install` (which
inject `git describe --tags --always --dirty` output at build time) to
report the exact git-describe version. A plain `go build`/`go install
./cmd/trader` run outside `make`, from a local git checkout, instead
falls back to Go's own module-version inference (already a real,
usable value on a modern toolchain) or, failing that, a
`devel+<revision>` placeholder — still traceable to the exact commit,
just not necessarily the exact git-describe string. A version-qualified module install
(`go install .../trader@v0.3.0`) reports that exact version alone,
with no commit info, since Go does not stamp VCS metadata for that
install form. See
[`cmd/trader/internal/version`](../cmd/trader/internal/version)'s own
doc comment for the complete precedence/fallback chain.

Trader follows semantic versioning while remaining at `v0` (ADR-011,
ADR-064 in [`docs/arch/adr-decisions.org`](arch/adr-decisions.org));
see [`CONTRIBUTING.org`](../CONTRIBUTING.org)'s "Releasing" section for
how a version is tagged and released.

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
├── backtest    run backtests and inspect their results
│   ├── run         run a backtest and render/persist its result
│   └── show        render a previously run backtest's persisted result
└── version     print Trader's version and build metadata (see Versioning above)
```

`trader completion` also exists (standard Cobra shell-completion
boilerplate) and isn't covered further here.

---

## `trader data` — Historical Market Data

`marketdata.Manager`'s CLI surface: query canonical bars, inspect coverage,
and plan/sync/build/update a dataset. Every `data` subcommand takes the same
two positional arguments and shares the same flag set.

**Usage:** `trader data <subcommand> INSTRUMENT INTERVAL --from ... --to ...`

INSTRUMENT is a plain symbol. For the default `oanda` provider (or any FX
provider) it is a 6-letter FX pair, e.g. `EURUSD`. For a non-FX provider such
as `alpaca` it is an equity or ETF ticker, e.g. `AAPL` or `SPY` — `--exchange`
and `--kind` are then both **required**, since a bare ticker does not name
its own listing exchange or asset kind the way an FX pair's symbol does.
INTERVAL is one of the values listed under `backtest run` below.

### Shared `data` flags

| Flag                | Default                                | Meaning                                                                  |
|---------------------|-----------------------------------------|---------------------------------------------------------------------------|
| `--from`            | —                                      | range start (`YYYY-MM-DD` or RFC3339), **required**                     |
| `--to`              | —                                      | range end (`YYYY-MM-DD` or RFC3339), **required**                       |
| `--format`          | `table`                                | `table` or `json`                                                       |
| `--provider`        | `oanda`                                | canonical dataset provider name (e.g. `oanda`, `alpaca`)                |
| `--raw-root`        | `$XDG_DATA_HOME/trader/raw/<provider>` | raw provider archive root                                                |
| `--store-root`      | `$XDG_DATA_HOME/trader/data`           | canonical data store root                                                |
| `--oanda-base-url`  | —                                      | OANDA API base URL; required only for `sync`/`update`                   |
| `--alpaca-base-url` | `https://data.alpaca.markets`          | Alpaca Market Data API base URL; only used with `--provider alpaca`     |
| `--exchange`        | —                                      | listing exchange (e.g. `ARCA`, `NASDAQ`); required for a non-FX provider |
| `--kind`            | —                                      | `equity` or `etf`; required for a non-FX provider                       |

The OANDA API token itself is never a flag — set the `TRADER_OANDA_TOKEN`
environment variable instead. Likewise, Alpaca's key ID and secret key are
never flags — set `TRADER_ALPACA_KEY_ID` and `TRADER_ALPACA_SECRET_KEY`
instead. Neither provider's secret belongs in shell history or a process
command line.

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
`--oanda-base-url` and `TRADER_OANDA_TOKEN` for the default `oanda`
provider, or `TRADER_ALPACA_KEY_ID`/`TRADER_ALPACA_SECRET_KEY` (plus
`--exchange`/`--kind`) for `--provider alpaca`:

```sh
trader data sync SPY D1 --provider alpaca --exchange ARCA --kind etf \
  --from 2024-01-01 --to 2024-02-01
```

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

| Flag              | Default           | Meaning                      |
|-------------------|-------------------|------------------------------|
| `--account-id`    | freshly generated | account id to use            |
| `--currency`      | `USD`             | account currency             |
| `--starting-cash` | `10000`           | starting account cash amount |

### `trader broker accounts`

Lists the simulated broker's accounts. `--format table\|json`.

### `trader broker snapshot`

Shows the simulated account's current snapshot (equity, positions, open
orders). `--format table\|json`.

### `trader broker submit`

Submits one order directly to the simulated broker (no sizing or risk
evaluation — see `trader execution submit` for that).

| Flag                   | Default   | Meaning                                        |
|------------------------|-----------|------------------------------------------------|
| `--symbol`             | —         | instrument symbol, e.g. `EURUSD`, **required** |
| `--side`               | —         | `buy` or `sell`, **required**                  |
| `--quantity`           | —         | order quantity, **required**                   |
| `--type`               | `market`  | `market`, `limit`, `stop`, or `stop-limit`     |
| `--price`              | —         | fill price, required for `--type market`       |
| `--limit-price`        | —         | required for `--type limit` or `stop-limit`    |
| `--stop-price`         | —         | required for `--type stop` or `stop-limit`     |
| `--tif`                | `GTC`     | time in force: `GTC`, `DAY`, `IOC`, or `FOK`   |
| `--tick-size`          | `0.00001` | simulator tick size                            |
| `--quantity-increment` | `1`       | simulator quantity increment                   |
| `--multiplier`         | `1`       | simulator contract multiplier                  |
| `--format`             | `table`   | `table` or `json`                              |

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

| Flag                   | Default   | Meaning                                               |
|------------------------|-----------|-------------------------------------------------------|
| `--symbol`             | —         | instrument symbol, **required**                       |
| `--side`               | —         | `buy` or `sell`, **required**                         |
| `--adverse-distance`   | —         | adverse price distance used for sizing, **required**  |
| `--risk-fraction`      | `0.01`    | fraction of account equity to risk (1%)               |
| `--reference-price`    | —         | valuation price for value-based risk rules (optional) |
| `--tick-size`          | `0.00001` | simulator tick size                                   |
| `--quantity-increment` | `1`       | simulator quantity increment                          |
| `--multiplier`         | `1`       | simulator contract multiplier                         |
| `--format`             | `table`   | `table` or `json`                                     |

### `trader execution submit`

Same as `evaluate`, plus an actual submission if risk approves. Adds:

| Flag      | Default | Meaning                                                 |
|-----------|---------|---------------------------------------------------------|
| `--price` | —       | fill price for the resulting market order, **required** |

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

There are three strategy paths:

- **Without `--config`/`--strategy-exec`:** a provisional demo strategy — one
  buy-and-hold entry per instrument's first bar. `--symbol` may be repeated
  for a multi-instrument run (one shared account/pipeline, not a per-symbol
  engine).
- **With `--config`:** the real `strategy/emacross` EMA-crossover strategy,
  for exactly one instrument, configured from a YAML file (see below). Any
  explicit flag still overrides its corresponding config-file value.
- **With `--strategy-exec`:** an out-of-tree strategy executable, launched
  and driven over Strategy Protocol v1 — see
  [External strategies](#external-strategies) below. Mutually exclusive
  with `--config`.

| Flag                 | Default                         | Meaning                                                                                |
|----------------------|---------------------------------|----------------------------------------------------------------------------------------|
| `--symbol`           | —                               | instrument symbol, repeatable, **required** (or via `--config`)                        |
| `--interval`         | `H1`                            | `M1`, `H1`, `H4`, `D1`, or `W1`                                                        |
| `--from`             | —                               | replay range start, **required** (or via `--config`)                                   |
| `--to`               | —                               | replay range end, **required** (or via `--config`)                                     |
| `--currency`         | `USD`                           | account currency                                                                       |
| `--starting-cash`    | `10000`                         | starting account cash amount                                                           |
| `--risk-fraction`    | `0.01`                          | fraction of account equity to risk                                                     |
| `--adverse-distance` | —                               | adverse price distance for sizing, **required** (or via `--config`)                    |
| `--warmup-bars`      | `0`                             | warm-up bars before the **demo** strategy may trade (ignored with `--config`)          |
| `--data-raw-root`    | —                               | raw archive root, **required**                                                         |
| `--data-store-root`  | `/srv/trading/data/canonical`\* | canonical data store root; an explicit empty value opts into a fresh temp dir per run  |
| `--provider`         | `oanda`                         | market data provider name                                                              |
| `--config`           | —                               | YAML file supplying backtest/strategy parameters (see below)                           |
| `--strategy-name`    | —                               | must equal `ema-cross` when `--config` is used                                         |
| `--fast-period`      | —                               | EMA fast period (only with `--config`)                                                 |
| `--slow-period`      | —                               | EMA slow period (only with `--config`)                                                 |
| `--allowed-side`     | `both`                          | restrict the EMA strategy: `both`, `long-only`, or `short-only` (only with `--config`) |
| `--strategy-exec`    | —                               | path to an out-of-tree strategy executable; mutually exclusive with `--config`         |
| `--strategy-args`    | —                               | extra argument passed to `--strategy-exec`'s own executable, unmodified; repeatable    |
| `--strategy-config`  | —                               | path to a config file for `--strategy-exec`'s own executable (see below)               |
| `--journal`          | —                               | optional path to write a durable JSONL audit trail; path must not already exist        |
| `--output-dir`       | `./backtest-runs`               | where run snapshots are written / `show` reads from                                    |
| `--format`           | `table`                         | `table`, `json`, or `org`                                                              |

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

Precedence for each of the fields shown above (the ones with a `config:`
tag backing them — see [Environment Variables](#environment-variables))
is: explicit CLI flag > `--config` file value > `TRADER_BACKTEST_*`/
`TRADER_STRATEGY_*` environment variable > the default shown above.
`--data-raw-root`, `--provider`, `--journal`, `--output-dir`, `--format`,
and `--warmup-bars` are plain CLI flags with no `--config`/environment-
variable backing at all — see the flag table above for which is which.

### External strategies

`--strategy-exec` runs an out-of-tree strategy executable instead of an
in-tree one, launched by `trader` itself and driven over Strategy Protocol
v1 — a gRPC protocol over a Unix-domain socket (ADR-062, ADR-063). The
executable's own `Describe()` — not `--symbol`/`--interval` — determines
the actual replay universe; `--symbol`/`--interval` only control what
canonical data `run` publishes beforehand, which must still cover whatever
the executable will request.

#### Building a Go external strategy

Import [`strategysdk`](../strategysdk) and implement its `Strategy`
interface (`Describe`/`Start`/`OnBar`, plus the optional `FillHandler`
capability). `strategysdk.Serve(yourStrategy)` is normally the entire body
of `main()`:

```go
func main() {
    if err := strategysdk.Serve(NewMyStrategy(cfg)); err != nil {
        log.Fatal(err)
    }
}
```

See [`examples/strategysdk-minimal`](../examples/strategysdk-minimal) for
the smallest complete, compiling example, and
[`examples/sma-long-hold`](../examples/sma-long-hold) for a real,
non-trivial one (SMA/indicator state, a ratcheting protective stop,
decision-evidence signals) that is proven byte-for-byte equivalent to its
in-tree counterpart, `strategy/smatrend`, by
`cmd/trader/backtest/sma_long_hold_equivalence_test.go`.

An external strategy never receives a broker handle, never evaluates risk,
and never submits an order directly — it only describes intents, exactly
like an in-tree `strategy.Strategy`. `strategysdk` itself, and every
strategy built on it, is architecturally barred from importing Trader's
`strategy`, `backtest`, `service`, `cmd`, `adapters`, `broker`, `execution`,
`risk`, or `pipeline` packages.

#### Running it

```sh
go build -o /tmp/my-strategy ./cmd/my-strategy

trader backtest run \
  --strategy-exec /tmp/my-strategy \
  --strategy-config my-strategy.json \
  --symbol EURUSD --interval H1 --from 2024-01-01 --to 2024-06-01 \
  --adverse-distance 0.0050 \
  --data-raw-root /path/to/raw/oanda
```

`--strategy-config`'s path is forwarded to the child process via the
`TRADER_STRATEGY_CONFIG` environment variable — a `trader`-owned
convention, not part of Strategy Protocol v1 itself, so a config file's
own schema is entirely the strategy author's choice (`json.Unmarshal` a
struct, parse YAML, whatever the strategy needs). `--strategy-args` passes
additional arguments straight through to the executable, unmodified.

#### Process lifecycle and logs

`trader` launches the executable, creates a fresh Unix-domain socket per
run, and waits for it to complete Strategy Protocol v1's Handshake before
the backtest replay begins; a child that fails to start, or exits before
completing Handshake, fails the run immediately with a clear error rather
than hanging. The child's own environment is inherited from `trader`'s own
process (`PATH`, `HOME`, credentials, etc.), not a stripped one. The
child's stderr is captured and logged as structured warning records under
`external strategy stderr`; its stdout is not touched. On success, `trader`
sends a normal-completion `SessionEnd` before terminating the process; on
any other exit path the process still receives `SIGTERM`, escalating to
`SIGKILL` after a grace period. A child that exits unexpectedly mid-run —
even between two strategy callbacks that would not otherwise have
surfaced the crash — is detected and reported as a run failure, never
silently treated as a successful backtest.

#### Protocol/version mismatch behavior

`trader` and the strategy negotiate a Strategy Protocol version and a
capability set (for example, whether the strategy implements
`FillHandler`) during Handshake. A version the host does not recognize, or
a capability the strategy's own guest-side runtime requires but the host's
accepted response omits, fails the Handshake explicitly — `run` reports
this as a clear startup error, never a silent degradation to a subset of
behavior.

#### Reproducibility

Every external run's persisted report records unambiguous provenance
under `run.strategy_parameters`, alongside the strategy's own
`run.strategy_name`/`run.strategy_version` (from its Handshake
`Descriptor`, the same fields any in-tree strategy's manifest carries):

```json
{
  "mode": "external",
  "strategy_name": "my-strategy",
  "strategy_version": "1.0.0",
  "protocol_version": "v1",
  "transport": "unix",
  "exec": "/tmp/my-strategy",
  "exec_digest": "sha256:...",
  "args": [],
  "config": "/home/you/my-strategy.json",
  "config_digest": "sha256:..."
}
```

`exec_digest`/`config_digest` are content digests of the executable file
and the `--strategy-config` file itself, both computed immediately before
launch — they distinguish two different builds behind the identical
`--strategy-exec` path, or two different config files behind the identical
`--strategy-config` path (for example, the same path edited in place
between two runs), neither of which a path/name alone can. `config_digest`
is empty when `--strategy-config` is not given. The ephemeral Unix-domain
socket path `trader` generates for that one run is never recorded anywhere
in this provenance: it has no reproducibility meaning and is specific to
that single process's lifetime.

#### v1 limitations / non-goals

- Only a local executable form is supported (`--strategy-exec /path/to/binary`
  plus `--strategy-args`); `unix://`/`grpc://` remote endpoint forms are not
  implemented in v1.
- Exactly one strategy executable per run; there is no multi-strategy or
  multi-process orchestration.
- No sandboxing beyond normal OS process isolation — an external strategy
  executable runs with the same OS-level privileges as the `trader`
  process that launches it (though never with broker/risk/execution
  access at the protocol level, per Strategy Protocol v1's own design).
- Config-file schema is entirely the strategy author's own responsibility;
  `trader` never parses or validates it.

### `trader backtest show <run-id>`

Renders a persisted run snapshot written by a prior `run` — no
recomputation, byte-identical to what `run` itself rendered.

| Flag           | Default           | Meaning                                     |
|----------------|-------------------|---------------------------------------------|
| `--output-dir` | `./backtest-runs` | directory the run's snapshot was written to |
| `--format`     | `table`           | `table`, `json`, or `org`                   |

```sh
trader backtest show run_01HKK5WY00D5982ACAHT01Q80K --format org
```

---

## Environment Variables

**Not every flag documented above is settable as an environment
variable.** Only fields actually loaded through `config.Load` (visible
by their own `config:` struct tag in the command's source) get an
environment-variable form; every other flag is a plain Cobra flag with
no config-file or environment-variable backing at all, no matter how
important it looks. Where a field *is* config-backed, the variable name
follows `config`'s naming convention: prefix `TRADER_`, then the dotted
config path, uppercased with `.` replaced by `_` — see the
[`config` package doc comment](../config/doc.go) for the full rule.

Concretely, per command:

- **`trader backtest run`** — only the fields shown in the `--config`
  YAML reference above are env-backed: `TRADER_BACKTEST_SYMBOL`,
  `_INTERVAL`, `_FROM`, `_TO`, `_CURRENCY`, `_STARTING_CAPITAL`,
  `_RISK_FRACTION`, `_ADVERSE_DISTANCE`, `_DATA_STORE_ROOT`, and
  `TRADER_STRATEGY_NAME`, `_FAST_PERIOD`, `_SLOW_PERIOD`,
  `_ALLOWED_SIDE`. `--data-raw-root`, `--provider`, `--journal`,
  `--output-dir`, `--format`, and `--warmup-bars` are **not**
  env-backed — flag only.
- **`trader data`** (all subcommands) — only the parent command's
  persistent flags are env-backed: `TRADER_STORE_ROOT`,
  `TRADER_RAW_ROOT`, `TRADER_PROVIDER`, `TRADER_OANDA_BASE_URL`. Each
  leaf subcommand's own `--from`/`--to`/`--format` are flag only.
- **`trader broker`** / **`trader execution`** — only the shared
  `--starting-cash`/`--currency`/`--account-id` flags are env-backed
  (`TRADER_STARTING_CASH`, `TRADER_CURRENCY`, `TRADER_ACCOUNT_ID`).
  Every leaf-specific flag (`--symbol`, `--side`, `--quantity`,
  `--type`, `--price`, `--adverse-distance`, `--format`, ...) is flag
  only.

These credentials are environment-only and have no flag at all:

| Variable                   | Meaning                                                          |
|-----------------------------|-------------------------------------------------------------------|
| `TRADER_OANDA_TOKEN`        | OANDA API token, required for `trader data sync`/`update`       |
| `TRADER_ALPACA_KEY_ID`      | Alpaca API key ID, required (with the secret key below) for `trader data sync`/`update --provider alpaca` |
| `TRADER_ALPACA_SECRET_KEY`  | Alpaca API secret key, required (with the key ID above) for `trader data sync`/`update --provider alpaca` |
