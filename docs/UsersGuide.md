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
commit-time: 2026-09-15T18:00:00Z
```

(`commit-time` is that commit's own timestamp, not when this
particular binary happened to be compiled — Trader records no
separate build-wall-clock timestamp.)

Both forms require a build made via `make build`/`make install` (which
inject `git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --always
--dirty` output at build time — restricted to Trader's own release-tag
namespace, so an unrelated repository tag can never become Trader's
own reported version) to report the exact git-describe version. A
plain `go build`/`go install ./cmd/trader` run outside `make`, from a
local git checkout, instead falls back to Go's own module-version
inference (already a real, usable value on a modern toolchain) or,
failing that, a `devel+<revision>` placeholder — still traceable to
the exact commit, just not necessarily the exact git-describe string.
A version-qualified module install (`go install .../trader@v0.3.0`)
reports that exact version alone, with no commit info, since Go does
not stamp VCS metadata for that install form. See
[`version`](../version)'s own
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

`internal/marketdata.Manager`'s CLI surface: query canonical bars, inspect coverage,
and plan/sync/build/update a dataset. Every `data` subcommand takes the same
two positional arguments and shares the same flag set.

**Usage:** `trader data <subcommand> INSTRUMENT INTERVAL --from ... --to ...`

INSTRUMENT is a plain symbol. For the default `oanda` provider (or any FX
provider) it is a 6-letter FX pair, e.g. `EURUSD`. For a non-FX provider such
as `alpaca` it is an equity or ETF ticker, e.g. `AAPL` or `SPY`. A bare ticker
does not name its own listing exchange or asset kind the way an FX pair's
symbol does, so `--exchange` and `--kind` are **required together** — except
for the reference symbols SPY (ARCA ETF), QQQ (NASDAQ ETF), and AAPL (NASDAQ
equity), which resolve without them. The same resolution is used by
`backtest run` and `trader-mcp`.
INTERVAL is one of the values listed under `backtest run` below.

### Shared `data` flags

| Flag                | Default                                | Meaning                                                                  |
|---------------------|-----------------------------------------|---------------------------------------------------------------------------|
| `--from`            | —                                      | range start (`YYYY-MM-DD` or RFC3339); **required**, except that `build`, `update`, and `coverage` accept omitting both ends (see each command) |
| `--to`              | —                                      | range end (`YYYY-MM-DD` or RFC3339); given together with `--from`        |
| `--format`          | `table`                                | `table` or `json`                                                       |
| `--provider`        | `oanda`                                | canonical dataset provider name (e.g. `oanda`, `alpaca`)                |
| `--raw-root`        | `$XDG_DATA_HOME/trader/raw/<provider>` | raw provider archive root                                                |
| `--store-root`      | `$XDG_DATA_HOME/trader/data`           | canonical data store root                                                |
| `--oanda-base-url`  | —                                      | OANDA API base URL; required only for `sync`/`update`                   |
| `--alpaca-base-url` | `https://data.alpaca.markets`          | Alpaca Market Data API base URL; only used with `--provider alpaca`     |
| `--exchange`        | —                                      | listing exchange (e.g. `ARCA`, `NASDAQ`); required for a non-FX provider unless the symbol has reference metadata |
| `--kind`            | —                                      | `equity` or `etf`; required with `--exchange`                           |

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

Builds and publishes canonical data from the provider's native data — never
fetches from a live provider. For `stooq` that is the native archive (under
`--archive-root`), falling back to raw data already imported when no archive is
found; for other providers it is the raw data already present under
`--raw-root`. Omit `--from`/`--to` to build the whole source span. A range with
no source data at all is an error, not a silent no-op. This is the same
operation `trader-mcp`'s canonicalize tool runs.

### `trader data update INSTRUMENT INTERVAL`

Runs plan, then sync, then build, as each step actually requires — the
one-command path to "make sure this dataset is current." Omit `--from`/`--to`
to update from the last canonical bar through now; that needs existing
canonical data (build it first). `stooq` has no live feed, so its update
re-converts the native archive and picks up whatever newer data it holds. This
is the same operation `trader-mcp`'s update tool runs.

### `trader data coverage INSTRUMENT INTERVAL`

Reports canonical/raw coverage and any gaps for the dataset over the given
range. `--from` and `--to` are optional here: omit both to report over the
dataset's existing canonical span. Before anything has been built that report
is empty rather than an error.

```sh
trader data coverage EURUSD H1          # the whole canonical span
```

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
- **With `--config`:** an in-process strategy selected by `strategy.name` in
  the YAML file (see below). Any explicit flag still overrides its
  corresponding config-file value.
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
| `--initial-margin-ratio` | `1`                         | equity required per unit of gross notional; `1` unlevered, `0.5` = 2×, `0.25` = 4× (see below) |
| `--warmup-bars`      | `0`                             | warm-up bars before the **demo** strategy may trade (ignored with `--config`)          |
| `--data-raw-root`    | —                               | raw archive root (required, or supplied by `backtest.data_raw_root`)                  |
| `--data-store-root`  | `/srv/trading/data/canonical`\* | canonical data store root; an explicit empty value opts into a fresh temp dir per run  |
| `--provider`         | `oanda`                         | market data provider name; `backtest.provider` in `--config` may supply it            |
| `--config`           | —                               | YAML file supplying backtest/strategy parameters (see below)                           |
| `--strategy-name`    | —                               | in-process strategy name selected by `--config`                                      |
| `--fast-period`      | —                               | EMA fast period for `ema-cross`                                                       |
| `--slow-period`      | —                               | EMA slow period for `ema-cross`                                                       |
| `--allowed-side`     | `both`                          | restrict `ema-cross`: `both`, `long-only`, or `short-only`                           |
| `--quantity`         | —                               | `buy-and-hold` quantity mode: buy exactly this quantity of the single `--symbol` (see below) |
| `--buy-date`         | first bar                       | quantity mode: buy on the first bar at or after this date                             |
| `--sell-date`        | —                               | quantity mode: exit on the first bar at or after this date; unset = hold to the run's end |
| `--strategy-exec`    | —                               | path to an out-of-tree strategy executable; mutually exclusive with `--config`         |
| `--strategy-args`    | —                               | extra argument passed to `--strategy-exec`'s own executable, unmodified; repeatable    |
| `--strategy-config`  | —                               | path to a config file for `--strategy-exec`'s own executable (see below)               |
| `--journal`          | —                               | optional path to write a durable JSONL audit trail; path must not already exist        |
| `--output-dir`       | `./backtest-runs`               | where run snapshots are written / `show` reads from; also `TRADER_BACKTEST_OUTPUT_DIR` or `backtest.output_dir` in `--config` (the flag wins, then the environment, then the file). `trader-mcp` resolves the same setting, so either can read the other's runs |
| `--format`           | `table`                         | `table`, `json`, or `org`                                                              |

\* The `/srv/trading/data/canonical` default is this repository's own local
operational choice (issue #268) — a fresh clone on another machine should
pass `--data-store-root` explicitly, or rely on the automatic temporary-
directory fallback by passing an explicit empty value.

```sh
# This sizing is about 2.2x equity, so it opts into up to 4x leverage;
# without --initial-margin-ratio the default (1, unlevered) rejects it.
trader backtest run \
  --symbol EURUSD --interval H1 --from 2024-01-01 --to 2024-06-01 \
  --starting-cash 10000 --risk-fraction 0.01 --adverse-distance 0.0050 \
  --initial-margin-ratio 0.25 \
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
  initial_margin_ratio: 1      # default 1 (unlevered); must be positive
  data_raw_root: /path/to/raw/oanda  # required
  data_store_root: /path/to/canonical  # default /srv/trading/data/canonical
  provider: oanda                    # default oanda; e.g. stooq

strategy:
  name: buy-and-hold            # registered in-process strategy; also supports ema-cross
  fast_period: 20              # used when name is ema-cross
  slow_period: 50              # used when name is ema-cross
  allowed_side: both           # used when name is ema-cross
  quantity: 8500               # buy-and-hold quantity mode (see below)
  buy_date: 2024-01-08         # quantity mode; default: the first bar
  sell_date: 2024-06-28        # quantity mode; optional
```

Precedence for each of the fields shown above (the ones with a `config:`
tag backing them — see [Environment Variables](#environment-variables))
is: explicit CLI flag > `--config` file value > `TRADER_BACKTEST_*`/
`TRADER_STRATEGY_*` environment variable > the default shown above.
`--journal`, `--format`, and `--warmup-bars` are plain CLI flags with no
`--config`/environment-variable backing at all — see the flag table above
for which is which. `--output-dir` is shared with `show` and `trader-mcp`:
see its row in the flag table.

`strategy.name` (or `--strategy-name`, or `TRADER_STRATEGY_NAME`) selects the
strategy however it is supplied; `--config` is not required to choose
`ema-cross`. The market data a run builds and reads uses the provider's own
trading calendar (for example the US equity calendar for `stooq` and
`alpaca`), as `trader data` does.

#### Initial margin (`initial_margin_ratio`)

Every backtest enforces an account-level **initial-margin** limit
(ADR-066, `docs/account-risk.md`):

```
prospective gross notional × initial_margin_ratio ≤ equity
```

- **Gross notional** sums every open position's
  `|quantity| × price × contract multiplier`, each at its current
  price; longs and shorts both add.
- The default `1` is unlevered: gross exposure may not exceed equity.
  `0.5` permits 2×, `0.25` permits 4×.
- One value configures two checks. They share the same ratio and the
  same calculation, but they evaluate different states, so they don't
  always reach the same outcome:
  - **Admission** (the `account_initial_margin` risk rule) checks the
    order at its reference price against equity before fees.
  - **Fill time** (the simulator) checks the actual fill price, after
    slippage, against equity after the fill's own commission. An order
    admitted earlier can still be refused here, for example after a
    next-bar gap up or because of its commission.
- An order that would breach the limit is **refused, never resized**.
  How it appears in the journal (`--journal`) depends on where it was
  refused:
  - At admission: a risk decision whose violation names
    `account_initial_margin`, with the required margin and equity.
  - A market order refused at fill: a broker order with status
    `rejected` and reason `insufficient_margin`.
  - A resting limit or stop order refused when it triggers: the broker
    cancels it, with reason `insufficient_margin` in its
    `cancel_reason`. There is no risk decision for this case.
- Orders that reduce or close a position are always allowed, even when
  the account is already over the limit.
- There is no maintenance margin: a position is never liquidated
  because prices later move against it. While the account is over the
  limit, only exposure-reducing orders are accepted.
- The ratio is recorded in the run manifest (`risk_rules`,
  `margin_model`) and changes `config_digest`.
- Every report (`table`, `json`, `org`) has a **Margin** section:
  - the configured ratio (`none` when a run has no margin model)
  - the number of refusals, split into admission and fill-time
  - the peak gross notional and peak gross leverage (gross notional ÷
    equity) observed across the run's equity curve

**Sizing and margin are separate controls.** `risk_fraction` sizes a
position from how much you're willing to lose at the adverse distance;
it does not check whether the account can finance that position. For
example, 1% risk over a 0.0050 adverse distance on $10,000 sizes
20,000 EUR/USD (about 2.2× equity), which the default ratio rejects.
Either size smaller or opt into leverage explicitly with a smaller
`initial_margin_ratio`. Runs made before this limit existed had no
margin cap at all, so no ratio reproduces them exactly.

#### Margin and sizing

Two separate questions decide whether a position is opened:

- **Sizing: "how much am I willing to lose?"** `risk_fraction` sizes a
  position from the loss you'll accept at the adverse distance. It says
  nothing about whether the account can pay for the result.
- **Margin: "can the account finance it?"** `initial_margin_ratio`
  (above) admits or refuses the order. It never resizes it.

#### Buy & Hold baseline (`buy-and-hold` quantity mode)

Setting `quantity` switches `buy-and-hold` from its fixed-fraction demo
behavior into the Buy & Hold baseline:

| Setting | Meaning |
|---|---|
| instrument | the run's single `--symbol` / `backtest.symbol` |
| `quantity` | exactly how much to buy; never sized or resized. Must be positive: an explicit `0` is an error, not a fallback to the demo |
| `buy_date` | buy on the first bar at or after it (default: `backtest.from`) |
| `sell_date` | optional: exit on the first bar at or after it, once the position is held; must be after the (effective) buy date |

- The strategy decides *what and when*. Margin decides whether that
  quantity is admissible, and the simulator decides the fill.
- An order is decided on a bar's close and fills at the **next bar's
  open**. The entry always comes first: if both dates have passed by
  the first available bar (for example over a weekend), it buys on
  that bar and exits on a later one.
- A quantity the account can't finance is rejected once, with a margin
  rejection in the report. It is not resized and not retried.
- **The run's end date is not a sell.** Without `sell_date` the position
  stays open in the final account state, valued at the final mark, and
  no closing trade is added.
- The quantity and dates are recorded in the run manifest as strategy
  parameters, so they change `config_digest`.

**Choosing a financeable quantity.** Pick one where

```
quantity × price × multiplier  ≤  starting cash ÷ initial_margin_ratio
```

with headroom for the fill. The fill happens at the next bar's open, not
the price you looked at, and any commission comes out of equity first.
For example, with $10,000 at the default ratio of 1 and EUR/USD near
1.10:

- 9,000 units is about $9,900: it fills only if the next open doesn't
  gap up more than about 1%.
- 8,500 units is about $9,350, leaving about 6.5% headroom, which is a
  reasonable baseline.

If the report shows a margin rejection and no trades, the quantity was
too large for the account.

### External strategies

`--strategy-exec` runs an out-of-tree strategy executable instead of an
in-tree one, launched by `trader` itself and driven over Strategy Protocol
v1 — a gRPC protocol over a Unix-domain socket (ADR-062, ADR-063). The
executable's own `Describe()` — not `--symbol`/`--interval` — determines
the actual replay universe; `--symbol`/`--interval` only control what
canonical data `run` publishes beforehand, which must still cover whatever
the executable will request.

#### Building a Go external strategy

Import [`sdk`](../sdk) and implement its `Strategy`
interface (`Describe`/`Start`/`OnBar`, plus the optional `FillHandler`
capability). `sdk.Serve(yourStrategy)` is normally the entire body
of `main()`:

```go
func main() {
    if err := sdk.Serve(NewMyStrategy(cfg)); err != nil {
        log.Fatal(err)
    }
}
```

See [`examples/sdk-minimal`](../examples/sdk-minimal) for
the smallest complete, compiling example, and
[`examples/sma-long-hold`](../examples/sma-long-hold) for a real,
non-trivial one (SMA/indicator state, a ratcheting protective stop,
decision-evidence signals) that is proven byte-for-byte equivalent to its
in-tree counterpart, `strategy/smatrend`, by
`cmd/trader/backtest/sma_long_hold_equivalence_test.go`.

An external strategy never receives a broker handle, never evaluates risk,
and never submits an order directly — it only describes intents, exactly
like an in-tree `strategy.Strategy`. `sdk` itself, and every
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
| `--output-dir` | `./backtest-runs` | directory the run's snapshot was written to; also `TRADER_BACKTEST_OUTPUT_DIR` |
| `--format`     | `table`           | `table`, `json`, or `org`                   |

```sh
trader backtest show run_01HKK5WY00D5982ACAHT01Q80K --format org
```

---

## `trader-mcp` — MCP Server

`trader-mcp` serves Trader's research tools to MCP clients (Claude,
Codex, and others) over stdio. Build it with
`go build ./cmd/trader-mcp`, then register the binary as a stdio MCP
server in your client.

It resolves configuration exactly as `trader data` does: the same
`TRADER_*` environment variables, default data roots, and credentials.

| Flag | Environment | Meaning |
|---|---|---|
| `--store-root`, `--raw-root`, `--archive-root` | `TRADER_STORE_ROOT`, `TRADER_RAW_ROOT`, `TRADER_ARCHIVE_ROOT` | data roots, as for `trader data` |
| `--provider` | `TRADER_PROVIDER` | default provider (`oanda`, `alpaca`, or `stooq`) for requests that don't name one |
| `--oanda-base-url`, `--alpaca-base-url` | `TRADER_OANDA_BASE_URL`, `TRADER_ALPACA_BASE_URL` | provider endpoints |
| — | `TRADER_OANDA_TOKEN`, `TRADER_ALPACA_KEY_ID`, `TRADER_ALPACA_SECRET_KEY` | credentials (environment only; never logged or returned) |
| `--allow-writes` | `TRADER_MCP_ALLOW_WRITES` | enable data-mutating tools (default off) |
| `--backtest-output-dir` | `TRADER_BACKTEST_OUTPUT_DIR` | where backtest runs are saved and read (default `./backtest-runs`, shared with `trader backtest run`/`show`) |
| `--log-level`, `--log-format`, `--log-output` | `TRADER_LEVEL`, `TRADER_FORMAT`, `TRADER_OUTPUT` | logging; output is stderr or a file, never stdout |

- **Write access is off by default.** Tools that change data, by
  writing to the raw or canonical store or downloading with your
  credentials, refuse to run unless the server was started with
  `--allow-writes`. Read-only tools always work.
- A raw or archive root you configure applies to the default provider
  only. Requests for another provider use that provider's own default
  under the Trader data directory, because raw data is
  provider-specific.
- **stdout is the protocol stream.** `--log-output stdout` is rejected,
  and logs default to stderr.

### Tools

| Tool | Access | What it does |
|---|---|---|
| `trader_version` | read | the Trader build serving the session |
| `trader_instruments` | read | resolve `symbols` for a `provider`: instrument ID, kind (`fx`, `equity`, `etf`), exchange, and the provider's symbol |
| `trader_marketdata_coverage` | read | for `symbols` at an `interval` (`M1`, `H1`, `H4`, `D1`, `W1`): monthly partitions and their status, gaps, and the raw and canonical data held. `from`/`to` are optional; omit both to cover each symbol's existing canonical data |
| `trader_marketdata_canonicalize` | write | build canonical data for `symbols` at an `interval` from data already held: a stooq archive under the archive root, or raw data in the raw store. Never downloads. Omit `from`/`to` for each symbol's whole source; `force` rebuilds current partitions. Same operation as `trader data build` |
| `trader_run_backtest` | read* | run a backtest with an in-process strategy (`buy-and-hold` or `ema-cross`), as `trader backtest run` does with the same inputs and defaults, and return its run ID, config digest, and a summary (run, dataset, performance, trade statistics, margin, account, trade counts). The run is saved to the backtest output directory |
| `trader_backtest_result` | read | a saved run's full report by run ID — the record `trader backtest show` reads, so either can read the other's runs |
| `trader_marketdata_update` | write | bring canonical data forward: oanda and alpaca download new raw data; stooq has no live feed and re-reads its archive. Omit `from`/`to` to update from each symbol's last canonical bar through now (canonicalize first). Same operation as `trader data update` |

The write tools return a result per symbol (`built`, `updated`, `current`, or
`failed`, with the source used, the range acted on, canonical data before and
after, and what was published) plus a summary with counts and `ok`. A failing
symbol never fails the call. A client that sends a progress token receives a
progress notification as each symbol finishes. Writes for one provider run one
at a time, even across concurrent tool calls, so two overlapping updates can
never lose each other's data; a call simply waits its turn. A typical workflow:

\* `trader_run_backtest` always saves its report, but builds canonical market
data first only with `--allow-writes`; without it, the run's data must already
be current or the call fails saying so (run `trader_marketdata_canonicalize`).
External strategy executables and journal files are not available over MCP.
A backtest runs synchronously: the call returns when the run finishes, sending
a progress notification per stage (preparing market data, running, saved) to a
client that supplies a progress token. A client may cancel a run it no longer
wants; set the client's tool timeout to cover your longest expected run.

```text
trader_marketdata_coverage   → what exists
trader_marketdata_update     → bring it current (or canonicalize for a new archive)
trader_marketdata_coverage   → confirm
trader_run_backtest          → run it; trader_backtest_result for the full report
```

Every market-data tool takes a list of symbols and returns one result per
symbol, so an unknown symbol is reported on its own entry without hiding
the others. A bad interval, date, or provider, or an empty symbol list,
fails the whole call. `provider` defaults to the server's. FX providers
take 6-letter pairs (`EURUSD`); equity providers currently take the
reference symbols `SPY`, `QQQ`, and `AAPL`. Results are the same as
`trader data coverage` reports for the same data, and no tool returns
bars.

Error messages a client sees describe only what it sent (a bad symbol,
interval, date, or provider). Any other failure, such as a storage or I/O
error, is reported as "... failed; see the trader-mcp server log", with the
details logged on the server, so tool results never reveal file paths or
configuration.

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
[`config` package doc comment](../internal/config/doc.go) for the full rule.

Concretely, per command:

- **`trader backtest run`** — only the fields shown in the `--config`
  YAML reference above are env-backed: `TRADER_BACKTEST_SYMBOL`,
  `_INTERVAL`, `_FROM`, `_TO`, `_CURRENCY`, `_STARTING_CAPITAL`,
  `_RISK_FRACTION`, `_ADVERSE_DISTANCE`, `_INITIAL_MARGIN_RATIO`, `_DATA_STORE_ROOT`, `_DATA_RAW_ROOT`, and
  `TRADER_STRATEGY_NAME`, `_FAST_PERIOD`, `_SLOW_PERIOD`,
  `_ALLOWED_SIDE`, `_QUANTITY`, `_BUY_DATE`, `_SELL_DATE`, plus
  `TRADER_BACKTEST_OUTPUT_DIR` (shared with `show` and `trader-mcp`).
  `--provider`, `--journal`, `--format`, and `--warmup-bars` are **not**
  env-backed — flag only.
- **`trader data`** (all subcommands) — only the parent command's
  persistent flags are env-backed: `TRADER_STORE_ROOT`,
  `TRADER_RAW_ROOT`, `TRADER_ARCHIVE_ROOT`, `TRADER_PROVIDER`, `TRADER_OANDA_BASE_URL`. Each
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

### `trader data stq2bars <symbol>`

Converts one native Stooq daily ZIP into Trader managed raw partitions and
canonical D1 bars. The command imports the source before checking/building, so
refreshed ZIP contents participate in the normal raw fingerprint and stale-data
semantics.

```sh
bin/stq2bars SPY
bin/stq2bars SPY --from 2010-01-01 --to 2020-01-01
bin/stq2bars SPY --rebuild
```

The provider defaults to `stooq` when no provider is configured, and the range
defaults to the archive's actual first and last dates. Set `TRADER_ARCHIVE_ROOT`
or pass `--archive-root` to locate native ZIPs; `--archive` selects one ZIP
explicitly. SPY, QQQ, and AAPL have reference listing metadata. Other symbols
must provide `--exchange` and `--kind`. Extraction is temporary and source ZIPs
are never modified.
