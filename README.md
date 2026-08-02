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

## Documentation

- [Framework requirements](docs/arch/trader-framework-requirements.org)
- [Framework architecture](docs/arch/trader-framework-architecture.org)
- [Architecture Decision Records](docs/arch/adr-decisions.org)
- Package boundaries (planned: `docs/arch/package-boundaries.org`)
- [Contribution guide](CONTRIBUTING.org)
- [Workflows](docs/workflows/workflows.org)
- [Testing](docs/workflows/testing.org)

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
