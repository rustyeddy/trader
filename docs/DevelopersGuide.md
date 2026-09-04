# Trader Developer's Guide

This guide tours Trader's public packages, organized by architectural layer
(foundation → domain → orchestration → adapters → applications), matching the
dependency direction described in
[the framework architecture document](arch/trader-framework-architecture.org).
Each section names the package's responsibility, links to its own package doc
comment for the full contract, and includes a short example where one clarifies
the API faster than prose.

See the [README](../README.md) for project status, design principles, and
getting-started instructions. See
[Architecture Decision Records](arch/adr-decisions.org) for the durable
decisions behind each boundary mentioned here.

## Foundation Packages

These packages have no dependency on Trader's trading domain. They are used
throughout the rest of the tree.

### num

`num` provides Trader's exact numeric types — `Price`, `Quantity`, `Rate`,
`Currency`, and `Money` — backed by scaled integers instead of binary
floating point, so authoritative values never suffer float rounding error.
Construct values from decimal text, then use their checked arithmetic
methods; malformed input and cross-currency mistakes are caught as errors
rather than silently miscomputed:

```go
price, err := num.ParsePrice("108.473")
qty := num.MustParseQuantity("100")
fee, err := price.MulRate(num.MustParseRate("0.001"))
```

Analytical code (indicators, statistics) that needs `float64` crosses through
one sanctioned, direct conversion — `Price.Float64()` — never a
serialize/reparse round-trip through decimal text; see
[ADR-045](arch/adr-045-analytical-float64-conversion-boundary.org). See
[ADR-004](arch/adr-004-exact-numeric-representation.org) for the full exact
numeric design rationale.

### id

`id` provides Trader-owned identifiers — `RunID`, `OrderID`, `FillID`,
`EventID`, `CorrelationID`, `IntentID`, and `AccountID` — each a distinct Go
type at compile time, backed internally by a monotonic ULID (time-sortable,
generated from an injected `clock.Clock`) but never exposing that as a
public dependency:

```go
g := id.NewGenerator(clock.Real{}, id.Random{})
order, err := id.GenerateOrderID(g)
// order.String() == "ord_01J8Z3K5H7T1MDCE9WNRP2VXY0"
```

`id.Metadata` traces a value through a multi-stage workflow — one
`CorrelationID` shared throughout, each stage's `CausationID` pointing at
the `EventID` immediately before it. See the
[package doc comment](../id/doc.go) for the full identifier list and the
worked intent → proposal → order → fill example.

### clock

`clock` is Trader's deterministic time seam: domain and application code
receives a `clock.Clock` instead of calling `time.Now`/`time.NewTimer`
directly, so backtests and simulations can advance time manually with no
wall-clock waiting. `Real` wraps the standard library for production;
`Simulated` advances only when told to:

```go
c := clock.NewSimulated(start)
timer := c.NewTimer(5 * time.Second)

c.Advance(10 * time.Second)
deadline := <-timer.C() // ready immediately, no sleep
```

See [ADR-015](arch/adr-015-deterministic-time-and-clock-abstraction.org)
for the full design rationale, including the precise equal-deadline
ordering and UTC/monotonic-metadata guarantees.

### config

`config` assembles typed application configuration for Trader's executable
composition roots, resolving each field from defaults, a YAML file, the
environment, and command-line overrides, in that precedence order. Define a
struct with tags for defaults, required fields, and secrets, then load it in
one call:

```go
type Settings struct {
    Port     int    `default:"8080"`
    APIKey   string `required:"true" secret:"true"`
}

cfg, err := config.Load[Settings](config.Options{EnvPrefix: "TRADER"})
```

See the [package doc comment](../config/doc.go) for the tag reference and
environment-variable naming convention.

### logging

`logging` builds Trader's structured loggers on top of `log/slog` — text or
JSON output, a configurable level, and canonical attribute names
(`CorrelationID`, `OrderID`, `AccountID`, ...) so records stay correlatable
across components. It is not a wrapper around `slog.Logger`: components
accept and use `*slog.Logger` directly.

```go
logger, closer, err := logging.New(logging.Config{Format: "json"})
defer closer.Close()

logger.Info("order placed", logging.OrderID, "abc123", "password", logging.Secret(pw))
```

`logging.Config` works directly with `config.Load` — `slog.Level` already
implements the same text encoding `config` expects. See the
[package doc comment](../logging/doc.go) for context propagation, redaction,
and the `Discard`/`Capture` test helpers.

## Domain Packages

These packages define Trader's trading vocabulary — instruments, market
data, orders, accounts, and strategies — independent of any particular
broker, data vendor, or deployment shape.

### instrument

`instrument` separates *what an economic thing is* (`Instrument`) from *how
a venue exposes it for trading* (`Listing`). Unlike `id`'s generated
identifiers, `instrument.ID` is canonical and deterministic — two provider
adapters parsing different spellings of the same pair resolve to the same
identity:

```go
eur, usd := num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")
eurUsd, err := instrument.NewCurrencyPair(eur, usd)
// eurUsd.ID().String() == "fx:EUR/USD", regardless of whether the
// provider spelled it "EUR_USD", "EURUSD", or "EUR/USD"

dec, err := instrument.NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
mar, err := instrument.NewFuture("ES", time.Date(2027, time.March, 20, 0, 0, 0, 0, time.UTC))
// dec and mar are distinct Instruments: an individual expiring contract,
// not the contract family, is the Instrument
```

Initial kinds are currency pairs, equities, ETFs, individual futures
contracts, non-orderable continuous research series, and indices. `Listing`
keeps provider (a broker or data vendor, e.g. `"IBKR"`) distinct from venue
(an exchange, e.g. `"NASDAQ"`) and rejects `Tradable: true` for a
non-orderable `Instrument` — see the
[package doc comment](../instrument/doc.go) for why futures split into two
kinds instead of one, and why synthetic/multi-leg instruments are deferred.

`instrument.Resolver` resolves provider symbols and `Instrument`+venue
combinations to `Listing`s without ever letting a symbol or alias become
identity. `MemoryResolver` is the in-memory reference implementation — a
plain value with no package-level registry, so independent instances need
no special setup:

```go
r := instrument.NewMemoryResolver()
r.Register(oandaListing)
r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD")

listing, err := r.ResolveSymbol("OANDA", "", "EURUSD") // resolves via the alias
// listing.Symbol() == "EUR_USD" -- the alias never becomes the identity
```

An unconstrained provider/venue that matches more than one `Listing`
reports `ErrAmbiguousSymbol` rather than picking one; no match reports
`ErrUnknownSymbol`. See [ADR-016](arch/adr-decisions.org) and the
[package doc comment](../instrument/doc.go) for the full resolution
semantics.

### marketdata

`marketdata` is the closed market-data subsystem (ADR-020): the sole
access point for canonical bars/quotes/trades, acquisition from provider
raw archives, canonical storage, and coverage tracking. `Manager` is the
only exported entry point — providers, storage, and normalization live
under `marketdata/internal/` and are unreachable from outside the package.

```go
mgr, err := marketdata.New(marketdata.Config{
    Clock: clock.Real{}, StoreRoot: "./canonical",
    RawRoot: "/data/raw/oanda", Resolver: resolver, ProviderName: "oanda",
})

plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurUsd, Interval: marketdata.H1, Range: span})
if len(plan.Actions) > 0 {
    _, err = mgr.Build(ctx, plan) // acquire/normalize missing canonical data
}
reader, err := mgr.Bars(ctx, query) // Reader[Bar]: Next(ctx)/Close()
```

`Interval` is built from a typed unit and count, never parsed from a
provider string, with five predefined values
(`marketdata.M1/H1/H4/D1/W1`). `Calendar` aligns a `time.Time` to sessions
and bar boundaries; minute and hour bars align to the daily FX rollover
(not UTC midnight — see ADR-021 in [the ADR registry](arch/adr-decisions.org)),
while day and week bars align to the Sunday 17:00 New York session open,
with `time.Date`-based arithmetic so daylight-saving transitions resolve
correctly. See [ADR-012](arch/adr-decisions.org),
[ADR-020](arch/adr-020-historic-data.org), and the
[package doc comment](../marketdata/doc.go) for the full half-open range
convention, coverage/gap model, and `AdjustmentMode`/`FetchPolicy`
semantics.

### indicator

`indicator` owns mathematical transforms over price series — currently
just the streaming exponential moving average the EMA crossover strategy
needs, not a general indicator framework. Indicator arithmetic is
`float64` throughout (the architecture's sanctioned analytical domain);
`indicator` never imports `num` or decides whether to buy or sell:

```go
ema, err := indicator.NewEMA(20) // SMA-seeded warm-up
err = ema.Update(price.Float64())
if ema.Ready() {
    value := ema.Value()
}
```

See the [package doc comment](../indicator/doc.go) for warm-up/readiness
semantics and the deterministic-replay guarantee.

### order

`order` defines Trader's broker-neutral order and execution vocabulary —
what a proposal, request, accepted order, and fill are, not order-state
transition rules or broker I/O. The stages are distinct types, not
variations on one struct:

```go
proposal, err := order.NewProposal(order.Proposal{ /* ... */ })
request, err := order.NewRequest(proposal, orderID) // orderID doubles as the idempotency key
```

`Order` embeds its originating `Request` (the requested values) alongside
separate `Accepted*` fields (what the broker actually accepted, which may
differ due to broker-side normalization); `RemainingQuantity` derives
from the accepted quantity, never the requested one. `Fill` preserves
both Trader's `FillID` and the broker's own `BrokerFillID`, plus its own
`AccountID`, so it never requires a join through the parent order.

`order` also validates lifecycle transitions: `Status.CanTransitionTo` is
the legal transition graph, and named `Apply*` functions
(`ApplyAcceptance`, `ApplyFill`, `ApplyCancelRequest`/`ApplyCancelResult`,
`ApplyReplaceRequest`/`ApplyReplaceResult`, `ApplyExpiration`,
`ApplyRejection`) apply one event to an `Order`, checking both the graph
and that the event's identity actually matches the order. Redelivering an
unchanged status is always a safe no-op; a duplicate `Fill` (by
`BrokerFillID` or `FillID`) is detected and returns the order unchanged.

`order.Intent` (`IntentEnter`, `IntentExit`, `IntentAdjustStop`,
`IntentTargetExposure`) is what a strategy emits, and `order.Trade` is the
derived entry/exit grouping backtest and report consume. See
[ADR-005](arch/adr-005-strategy-emits-intents.org),
[ADR-017](arch/adr-017-order-execution-vocabulary.org),
[ADR-018](arch/adr-018-order-lifecycle-transitions.org), and the
[package doc comment](../order/doc.go) for the full model.

### account

`account` models one broker account's authoritative observed snapshot
(ADR-019): an immutable value describing what a broker reported at one
point in time. `Snapshot` performs no I/O and implies nothing about
freshness beyond its own `AsOf`; it is deliberately distinct from a
broker-scoped action handle (`broker.Account`, see below). Positions,
open orders, and cash balances are deep-copied and revalidated through
`order`'s own constructors on the way in, so a caller cannot hand
`NewSnapshot` an already-invalid nested value. See the
[package doc comment](../account/doc.go).

### portfolio

`portfolio` models a Trader-level view spanning one or more
`account.Snapshot` values (ADR-019), preserving each contributing
account's provenance rather than collapsing accounts into an opaque
total. Currency conversion is explicit and caller-supplied — `portfolio`
never fetches rates itself — and an account whose currency has no
matching `ConversionRate` makes the whole portfolio's `Equity` report
`ConversionIncomplete` rather than a silently partial sum. See the
[package doc comment](../portfolio/doc.go) for the full aggregation and
exposure-grouping rules.

### strategy

`strategy` defines Trader's broker-neutral strategy runtime contract
(ADR-005): how a strategy receives market observations and emits
`order.Intent` values, without knowing whether it is running inside a
backtest or a future live session. The contract is deliberately small —
`Describe`, `Start`, `OnBar` — with optional capabilities (tick/fill/
account-event handlers, state snapshot/restore) added only when a real
consumer needs them. A strategy never imports `broker`, `execution`,
`risk`, or `pipeline`; `Environment.Intents` is the only way it
constructs a canonical `Intent`, guaranteeing deterministic
IDs/correlation without the strategy touching ID-generation machinery
directly. See the [package doc comment](../strategy/doc.go).

`strategy/emacross` is Trader's first real strategy: a fast/slow EMA
crossover against this contract, with a documented crossover/warm-up
state machine (see
[the EMA-01 research definition](research/ema-01-experiment-definition.org))
and an optional `AllowedSide` restriction (`both`/`long-only`/`short-only`,
issue #273) for isolating one direction's own performance.

## Execution, Risk, and Orchestration

These packages turn a strategy's intent into a broker submission, in three
independent stages composed by one orchestration seam.

### execution

`execution` defines Trader's execution-planning contract (ADR-006): the
seam that translates one `order.Intent` into a broker-neutral
`order.Proposal`, without approving risk or submitting anything to a
broker. `Planner.Plan` is narrow by design — it never fetches its own
account state, market data, or listing resolution, and it does not size
an `IntentEnter` itself (sizing is risk's job). `execution` depends only
on `order`, `account`, `instrument`, `num`, `id`, and `clock` — never on
`broker` or `risk`. See the [package doc comment](../execution/doc.go).

### risk

`risk` defines Trader's risk-admission contract (ADR-006, ADR-029): the
seam that admits or rejects one `order.Proposal` before it becomes an
`order.Request`. `Engine.Evaluate` is a strict approve/reject result,
never an adjusted proposal — every injected `Rule` runs, and a
`Decision` aggregates every rule's violations/warnings rather than
stopping at the first one. This package defines no concrete `Rule`
itself; per-trade loss, exposure/position limits, and leverage/margin
are each their own rule implementation. `risk` depends only on `order`,
`account`, and `context` — never on `broker` or `execution`. See the
[package doc comment](../risk/doc.go).

### broker

`broker` defines Trader's public broker/account contracts (ADR-007,
ADR-008) — the smallest stable ports needed by both the deterministic
simulated broker and future real broker adapters. `Broker` is a
session-level port (identity, account discovery, session lifecycle);
`Account` is the operational handle for actions and queries scoped to
one account. Every method takes and returns canonical `order`/`account`
values, never a provider-native type; `Account.Events` (ADR-024) streams
order/fill/account/status changes in deterministic, gap-free order. See
the [package doc comment](../broker/doc.go).

### pipeline

`pipeline` composes execution, risk, and broker submission into
Trader's one canonical M4 orchestration path (issue #185):

```
Intent -> risk.Sizer (IntentEnter only) -> execution.Planner ->
order.Proposal -> risk.Engine -> approved order.Request ->
broker.Account.Submit
```

`Pipeline.Evaluate` is the read-only prefix (runs every stage through
building the approved `Request`, never calls the broker); `Submit` is
its thin mutating continuation, so there is exactly one implementation
of the sizing → planning → risk → request-construction sequence, used
identically by backtest and any future live runtime. See the
[package doc comment](../pipeline/doc.go).

### journal

`journal` defines the storage-neutral contract for a durable, replayable
record of what happened during a run (ADR-036): the `Recorder` interface
and the `Record`/`Entry`/`Kind` model (intent, proposal, decision,
request, order, fill, account, status, trade, signal, run-started/
completed). Concrete storage — the JSONL adapter under
`adapters/journal/jsonl` — is a peer, not part of this package, so a
future SQLite/Postgres adapter never widens `journal`'s own surface.
`journal` depends only on `id`, `num`, `instrument`, `order`, `risk`,
`broker`, and `account` — never on `pipeline` or `backtest` — so it
remains reusable by a future live session. See the
[package doc comment](../journal/journal.go).

## Backtesting and Reporting

### backtest

`backtest` owns deterministic historical simulation orchestration
(ADR-035): it reads already-published canonical market data through
`marketdata.Manager`, merges it into one chronologically ordered replay
stream per strategy, and drives that stream through a simulation clock,
the strategy, and the same `pipeline.Pipeline` used by live trading,
recording results through `journal` and computing `Metrics` (return,
drawdown, profit factor, per-instrument and per-side breakdowns).
`backtest` never imports `adapters/broker/sim` or any other concrete
adapter — only the composition root (`cmd/trader/backtest`) constructs
adapters and injects ports. See
[ADR-035](arch/adr-035-m5-backtesting-architecture.org),
[ADR-041](arch/adr-041-backtest-determinism-suite.org) (the determinism
regression suite), and the [package doc comment](../backtest/doc.go).

### report

`report` renders backtest results without computing them (ADR-038): it
projects `backtest.Result` into a report-owned view model,
`BacktestReport`, once, then offers Org, text, and JSON renderers over
that same model. No renderer touches `backtest.Result`/`backtest.Metrics`
directly or performs arithmetic; the dependency runs one way only —
`backtest` never imports `report`. See the
[package doc comment](../report/doc.go).

## Application and Composition

### service

`service` is the root of Trader's application/service layer (ADR-022):
transport adapters (the CLI today, REST/WebSocket/SSE later) stay thin
by parsing input, building a request type from a `service` subpackage,
calling one service operation, and formatting the response. Each
subpackage — `service/marketdata`, `service/broker`, `service/execution`,
`service/backtest` — orchestrates one domain area and depends only on
public domain packages, never on another package's `internal/` tree.
See the [package doc comment](../service/doc.go).

## Adapters

Concrete implementations of the ports the domain packages define. Only
composition roots (`cmd/...`) import these directly.

- **`adapters/broker/sim`** — the deterministic simulated broker: a real
  `broker.Broker`/`broker.Account` implementation (not a special backtest
  case), with configurable fill/fee/slippage models
  ([ADR-028](arch/adr-028-simulator-fill-fee-slippage-models.org)) and
  limit/stop trigger semantics
  ([ADR-026](arch/adr-026-simulated-limit-stop-orders.org)).
- **`adapters/journal/jsonl`** — a `journal.Recorder`/`Reader` pair
  persisting one JSON object per line, with sequence assignment, fsync-on-
  close durability, and corrupt-entry detection on read
  ([ADR-036](arch/adr-036-backtest-journal.org)).

## Commands

`cmd/trader` is Trader's operator CLI, composed from thin per-area command
groups that each call one `service` subpackage:

- **`cmd/trader/data`** — sync/build canonical market data.
- **`cmd/trader/broker`** — inspect broker accounts.
- **`cmd/trader/execution`** — submit/evaluate order intents.
- **`cmd/trader/backtest`** — run a backtest (`run`) and re-render a
  persisted result (`show`); `--config` runs the real
  `strategy/emacross` strategy from a YAML file
  ([ADR-022](arch/adr-022-cli-app-service-layer.org),
  [ADR-040](arch/adr-040-backtest-cli.org)).

No command group contains strategy, execution, risk, or metric logic of
its own — mechanically enforced by each group's own `boundary_test.go`.

## Test Support

### tradertest

`tradertest` provides deterministic public builders and assertions for
code that consumes Trader's domain packages — `instrument`, `order`,
`account`, `portfolio` — so an external consumer's tests don't have to
reinvent "build me a valid EUR/USD listing" boilerplate. Every builder
takes a small params struct with defaults and returns the real domain
type via the real constructor (`order.NewProposal`, `account.NewSnapshot`,
...), never a parallel object model; a `Must`-prefixed variant panics on
error for terse setup. See the [package doc comment](../tradertest/doc.go).

### test/internal

Repository-internal-only numeric/quantity study helpers used by a few
packages' own tests. Not part of the public API.
