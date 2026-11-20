# Trader

Trader is a Go framework for strategy research and trading: exact numerics, a
market-data pipeline (download, raw store, canonical bars), a deterministic
backtest engine and a simulated broker.

## Project Status

Trader is in an early architectural phase and under active development. The
repository was restarted from a small, executable design; earlier, broader code
lives in [`_archive/`](_archive) as a donor and is ported one layer at a time. The
market-data layers (download, raw store, canonical bars) are being rebuilt first
(milestone "Market Data", issue #584).

**Trader is not ready for real-money trading.** Backtest simulation is available;
paper trading is planned; real-money execution is unavailable and is not
scheduled.

## Design Principles

- **Simple and understandable.** Concrete types, ordinary functions, small
  interfaces; no abstraction without a present use.
- **Deterministic by design.** Time, identifiers and data order are controllable,
  so a backtest is reproducible and a bar is usable only from its availability
  time.
- **Exact numbers.** Prices, quantities and money are exact fixed-point values
  (`num`), never floats on the authoritative path.
- **No invented data.** No synthetic or carried-forward bars. Bad source rows are
  kept and counted, never hidden.
- **Layered.** Transports call application services, which coordinate domain
  services and providers; one package owns all data-file I/O (ADR-091).
- **Secrets stay in the environment.** The OANDA token is read only from
  `TRADER_OANDA_TOKEN` (ADR-078).

## What it does today

```sh
make build                       # builds bin/trader (or: go build -o trader ./cmd/trader)

trader                           # backtest the EMAX scenarios and print a summary of each
trader data download ...         # download OANDA candles into the raw store
trader data build ...            # build canonical-v2 bars from the raw store
trader data status ...           # show which canonical months a range can load
trader data verify ...           # check canonical bars against the raw they were built from
trader data audit ...            # one integrity report for a range (text or JSON)
trader data migrate ...          # move an old-layout raw archive to the current layout (dry run unless --apply)
trader backtest ...              # backtest canonical bars from the data store
trader serve                     # read-only REST API over the canonical bars (docs/rest-api.md) and the loopback strategy service (ADR-094)
```

`trader data download` fetches bid/ask candles for a UTC range into month
partition files; `trader data build` converts them to canonical bars with a mid
price and a modeled availability time, keeping every rejected row as a bad record.
The commands, flags and configuration are in the [User's Guide](docs/UsersGuide.md).

### Getting started

1. `make build`, then `trader` to see the EMAX scenarios run (no data or token needed).
2. Put your OANDA token in the environment as `TRADER_OANDA_TOKEN` (never in a file
   or flag), choose a data root (`--data-root`, `TRADER_DATA_ROOT` or
   `marketdata.root` in `etc/trader.yml`; the default is `/srv/data`).
3. `trader data download --instrument EURUSD --interval h1 --from 2024-01-01 --to 2024-04-01`
4. `trader data build` with the same flags, then `trader data audit` to check the
   months, the gaps and the match with raw.
5. `trader backtest --instrument EURUSD --interval h1 --from 2024-01-01 --to 2024-04-01`

Details, every flag and what each report means are in the [User's Guide](docs/UsersGuide.md).

## Packages

| Package | Role |
|---|---|
| `trader` (module root) | the engine: orders, broker, account, strategy, scanner, scenarios |
| `cmd/trader` | the command line: flags, environment and config file; no domain logic |
| `service` | application services that coordinate providers and domain services |
| `marketdata` | public market-data model: `Bar`, `OHLC`, `SourcePartition` |
| `marketdata/service` | the market-data domain service: classification, availability, publishing |
| `marketdata/internal/canonical` | the canonical-v2 file format (private to `marketdata`) |
| `provider/oanda` | OANDA: raw store, REST client, raw-to-source mapping |
| `internal/store` | the one place data files are read and written; internal, so no other module can import it |
| `num`, `instrument` | exact numeric values; instrument identity |
| `internal/logging`, `internal/marketdata/layout` | structured logging; the data path resolver |

The dependency rules and their tests are in
[docs/arch/package-boundaries.org](docs/arch/package-boundaries.org) and the
[Developer's Guide](docs/DevelopersGuide.md).

## Documentation

- [User's Guide](docs/UsersGuide.md): commands, configuration, data layout
- [Developer's Guide](docs/DevelopersGuide.md): packages, layering, tests
- [Documentation index](docs/README.md): what is current and what is historical
- [Architecture Decision Records](docs/arch/adr-decisions.org)
- [Package boundaries](docs/arch/package-boundaries.org)
- [Market-data storage layout](docs/arch/market-data-storage.md)
- [Contribution guide](docs/CONTRIBUTING.org), [agent instructions](AGENTS.md)
- [Workflows](docs/workflows/workflows.org), [testing](docs/workflows/testing.org),
  [AI-assisted development](docs/workflows/ai-development.org),
  [reusable agent workflows](docs/workflows/ai-skills.org)

## Development

`make` wraps the Go toolchain:

- `make check`: the canonical verification, the same gates as CI, in order:
  `tidy-check`, `fmt-check`, `vet`, `lint`, `test`, `race` (see
  [Testing](docs/workflows/testing.org))
- `make fmt`, `make fmt-check`, `make tidy-check`, `make vet`, `make lint`
  (pinned `golangci-lint`), `make test`, `make race`, `make cover`
- `make build`, `make run` (the EMAX backtest), `make clean`
- `make smoke`: an opt-in live OANDA check, not part of `make check`; it needs
  `TRADER_OANDA_TOKEN` in your shell, writes only to a temporary directory and
  prints only the verdict (details in `bin/smoke.log`)

Every pull request keeps the EMAX summaries unchanged:
`go run ./cmd/trader 2>/dev/null | md5sum` must print
`195e457a21def1223e7181c86cb824a4`.

## License

Trader is distributed under the BSD 2-Clause License. See [LICENSE](LICENSE) for
the full text.

## Relationship to earlier code

Earlier Trader code, with its own package layout and broker coupling, is kept in
[`_archive/`](_archive) and in the repository history. It is a selective donor and
behavior reference: proven algorithms and fixtures are ported after being adapted
to the current architecture. Nothing in `_archive/` is built or imported.
