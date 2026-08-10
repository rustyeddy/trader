# Trader

Trader is a Go framework for algorithmic trading research and execution. It is
being designed as a modular, API-first library for:

- market research
- backtesting
- broker simulation
- paper trading
- eventual live execution

## Project Status

Trader is in an early, greenfield architectural phase. Core public packages,
adapters, and application services are still being established. The repository
is under active development.

**Trader is not ready for real-money trading.** Paper trading is the intended
default operating mode for any live-like runtime; real-money execution is not
available and is not scheduled as a milestone deliverable in the current plan.

## Design Principles

Trader's architecture is guided by a small set of principles:

- **Deterministic by design.** Time, identifiers, data order, and simulation
  models are controllable so that backtests are reproducible.
- **Paper trading by default.** A minimally configured live runtime never
  submits real-money orders; enabling real money requires an explicit,
  operator-visible decision.
- **Strategies emit intents, not broker orders.** Strategies describe what
  they want to accomplish; an execution layer translates intents into concrete
  orders.
- **Risk and execution are separate stages.** Risk decides whether a proposed
  exposure is acceptable; execution decides how to realize it.
- **Broker truth is reconciled into local state.** The broker is authoritative
  for orders, fills, positions, and balances; Trader maintains reconciled
  projections for speed, reporting, and recovery.

## Packages

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

See [ADR-004](docs/arch/adr-004-exact-numeric-representation.org) for the
full design rationale.

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

See the [package doc comment](config/doc.go) for the tag reference and
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
[package doc comment](logging/doc.go) for context propagation, redaction,
and the `Discard`/`Capture` test helpers.

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

See [ADR-015](docs/arch/adr-015-deterministic-time-and-clock-abstraction.org)
for the full design rationale, including the precise equal-deadline
ordering and UTC/monotonic-metadata guarantees.

### id

`id` provides Trader-owned identifiers — `RunID`, `OrderID`, `FillID`,
`EventID`, `CorrelationID`, and `AccountID` — each a distinct Go type at
compile time, backed internally by a monotonic ULID (time-sortable,
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
[package doc comment](id/doc.go) for the full identifier list and the
worked intent → proposal → order → fill example.

### instrument

`instrument` separates *what an economic thing is* (`Instrument`) from *how
a venue exposes it for trading* (`Listing`). Unlike `id`'s generated
identifiers, `instrument.ID` is canonical and deterministic — two provider
adapters parsing different spellings of the same pair resolve to the same
identity:

```go
eur, usd := num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")
eurUsd, err := instrument.NewCurrencyPair(eur, usd)
if err != nil {
	log.Fatal(err)
}
// eurUsd.ID().String() == "fx:EUR/USD", regardless of whether the
// provider spelled it "EUR_USD", "EURUSD", or "EUR/USD"

dec, err := instrument.NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
if err != nil {
	log.Fatal(err)
}
mar, err := instrument.NewFuture("ES", time.Date(2027, time.March, 20, 0, 0, 0, 0, time.UTC))
if err != nil {
	log.Fatal(err)
}
// dec and mar are distinct Instruments: an individual expiring contract,
// not the contract family, is the Instrument
```

Initial kinds are currency pairs, equities, ETFs, individual futures
contracts, non-orderable continuous research series, and indices. `Listing`
keeps provider (a broker or data vendor, e.g. `"IBKR"`) distinct from venue
(an exchange, e.g. `"NASDAQ"`) and rejects `Tradable: true` for a
non-orderable `Instrument` — see the
[package doc comment](instrument/doc.go) for why futures split into two
kinds instead of one, and why synthetic/multi-leg instruments are deferred.

`instrument.Resolver` resolves provider symbols and `Instrument`+venue
combinations to `Listing`s without ever letting a symbol or alias become
identity. `MemoryResolver` is the in-memory reference implementation —
a plain value with no package-level registry, so independent instances
need no special setup:

```go
r := instrument.NewMemoryResolver()
if err := r.Register(oandaListing); err != nil {
	log.Fatal(err)
}
if err := r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD"); err != nil {
	log.Fatal(err)
}

listing, err := r.ResolveSymbol("OANDA", "", "EURUSD") // resolves via the alias
// listing.Symbol() == "EUR_USD" -- the alias never becomes the identity
```

An unconstrained provider/venue that matches more than one `Listing`
reports `ErrAmbiguousSymbol` rather than picking one; no match reports
`ErrUnknownSymbol`. See [ADR-016](docs/arch/adr-decisions.org) and the
[package doc comment](instrument/doc.go) for the full resolution
semantics.

## Documentation

- [Framework requirements](docs/arch/trader-framework-requirements.org)
- [Framework architecture](docs/arch/trader-framework-architecture.org)
- [Architecture Decision Records](docs/arch/adr-decisions.org)
- [Package boundaries](docs/arch/package-boundaries.org)
- [Contribution guide](CONTRIBUTING.org)
- [Workflows](docs/workflows/workflows.org)
- [Testing](docs/workflows/testing.org)
- [M0 foundation review](docs/reviews/m0-foundation-review.org)

## Development

Trader uses a `Makefile` to wrap the standard Go toolchain. The available
targets are:

- `make fmt` — format sources with `go fmt ./...`
- `make fmt-check` — verify sources are already formatted
- `make vet` — run `go vet ./...`
- `make test` — run `go test ./...`
- `make race` — run tests with the race detector
- `make check` — run `fmt-check`, `vet`, `test`, and `race` (default target)

## License

Trader is distributed under the BSD 2-Clause License. See [LICENSE](LICENSE)
for the full text.

## Relationship to the Legacy Trader Repository

An earlier Trader implementation exists in a separate repository. The new
Trader treats it as a selective code donor and behavior reference: proven
algorithms and test fixtures may be transplanted after being adapted to the new
architecture. The legacy repository is **not** a build or runtime dependency of
the new Trader, and its package layout, configuration model, and broker
coupling are not carried forward.
