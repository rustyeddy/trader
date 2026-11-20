# Trader Developer's Guide

How the code is organized, how the layers fit and how to change them. The
decisions behind it are the ADRs ([registry](arch/adr-decisions.org)); the rules
every contributor follows are in [AGENTS.md](../AGENTS.md) and
[CONTRIBUTING.org](CONTRIBUTING.org). The guide for the earlier, larger runtime is
kept in [`_archive/docs/DevelopersGuide.md`](../_archive/docs/DevelopersGuide.md);
it does not describe this tree.

## The tree

```
cmd/trader/                transport: commands, flags, environment, config file
service/                   application services (marketdata.go)
marketdata/                public model: Bar, OHLC, MidOf, SourcePartition
  service/                 domain service: Build (classify, availability, publish)
  internal/canonical/      canonical-v2 files, availability rule (private)
provider/oanda/            OANDA: raw store, REST client, raw-to-source mapping
internal/store/              the only data-file I/O (internal: not importable from outside the module); storetest for tests
internal/marketdata/layout/  the one data path resolver
internal/logging/          structured logging on log/slog
num/  instrument/          exact numbers; instrument identity
(root package trader)      engine: orders, broker, account, strategy, scenarios
_archive/                  earlier code and docs, a donor; never imported
```

## Layering

```
cmd/trader  ->  service  ->  marketdata/service  ->  marketdata/internal/canonical  ->  store
                   |
                   +------>  provider/oanda  ->  store
```

- **Transport** (`cmd/trader`) parses flags, reads the environment and the config
  file, sets the store's root and calls an application service. It imports no
  provider: for a download it hands `service.Download` the base URL, the request
  gap and a `service.Credential` that reads the token from the environment on each
  request, and the service builds the client (a test enforces the import rule). It contains no
  domain logic and is the only place that reads the environment (ADR-078).
- **Application service** (`service`) carries out one use case by coordinating a
  provider and a domain service. It reads no environment. Today there is one,
  `BuildCanonical`; a package per service waits until the services are not small.
- **Domain service** (`marketdata/service`) owns the domain's rules. Domain
  services never call each other. `marketdata/internal` may be imported only from
  `marketdata/service` (Go's `internal` rule, plus an architecture test).
- **Provider** (`provider/oanda`) is one external system: its HTTP client, its raw
  file format, its symbols. It parses and maps; it applies no domain rule. It
  imports only public models, `store`, `layout`, `num` and `instrument`.
- **Store** (`store`) reads and writes bytes at paths relative to its root:
  `Read`, `Write` and `Create` and `Update` (atomic, under a per-path lock), and
  `List`. Nothing else opens, renames or walks data files; a test enforces it.
- **Public models** (`marketdata`, `num`, `instrument`) import no service,
  transport or provider.

Each boundary has an architecture test that fails when it is crossed; when you add
a boundary, add its test and prove it fails (see AGENTS.md).

## The market-data pipeline

```
download:  OANDA REST --provider/oanda--> raw partition  (<root>/oanda/raw/...)
build:     raw partition --ReadSourcePartition--> marketdata.SourcePartition
             --marketdata/service.Build--> canonical file (<root>/oanda/canonical/...)
```

- A raw partition is the provider's own format (raw-v1). It stores only complete
  candles. A complete row is never changed; a forming row stored by an older download
  is replaced by the closed candle when the provider reports it (a forming candle is
  not a fact), and nothing else is rewritten.
- `SourcePartition` carries what a bare `[]Bar` would lose: the raw row numbers,
  the provider's complete flag and the fingerprint of the raw bytes. Its bars are
  unvalidated: the provider sets no mid and no `AvailableAt`.
- `Build` classifies each row (incomplete, misaligned, invalid, crossed, duplicate,
  out of order), derives the mid and the availability time, keeps every rejected
  row as a bad record, and publishes. `canonical.Publish` writes nothing when the
  same build (raw fingerprint, builder, validator, availability policy) is already
  there.
- Reading is the other direction: `marketdata/service.Load` reads the canonical
  files of a range and returns a `marketdata.Series` (one instrument's bars at one
  interval, strictly ascending by time and by availability) with the state of every
  month: `loaded`, `bad` (has bad records), `missing`, or `stale` (another build in
  force, never used). Strict by default: a stale month always fails the load, and a
  missing or bad one fails it unless the request allows it. `AvailableAt` is the
  stored one, never recomputed. `service.LoadSeries` is the application-service
  path; `trader data status` is its first consumer.
- `marketdata/service.Verify` (`trader data verify`, ADR-090 section 8) checks a file
  against its raw partition independently of `Build`'s code; `service.VerifyCanonical`
  reads the raw (`oanda.ReadSourcePartition`), calls it per month, and `--sample`
  picks months evenly (`sampleMonths`).
- `rest` (`trader serve`, [REST API](rest-api.md)) is the HTTP transport: it normalizes
  the query parameters, calls `service.ReadBars` and writes JSON; reading, paging, the
  partial-by-default policy and the report of what is missing are `ReadBars`' (which
  uses `Load` with `SkipStale`). `rest` imports only `service` and the public
  `marketdata` model, and no provider (a test enforces it); `cmd/trader/serve.go` reads
  the environment (`TRADER_API_TOKEN`) and refuses a non-loopback address without it.
- `strategy`, `strategy/proto` and `strategy/server` are the public side of external
  strategies (ADR-094). `strategy` is the seam a strategy implements and the requested
  `Order`; `strategy/proto` is the wire contract and its strict codec; `strategy/server`
  is the host: a loopback TCP listener, registration, the RunID and the lock-step
  session. The server imports only `strategy` and `strategy/proto` (tests enforce it, and
  that no guest package imports it); the engine is attached through the `Admit` and
  `Handle` callbacks (#703). `trader serve` starts it on `127.0.0.1:8081`
  (`--strategy-listen host:port` changes the address, `off` turns it off; a non-loopback
  address is refused). Until the engine is attached every registration is refused.
- `service.MigrateLayout` (`trader data migrate`, ADR-092) moves `raw/<source>` to
  `<source>/raw` with `store.Move` (one rename, never overwrites), the mapping in
  `layout.LegacyRawDir` and verification by `store.Inventory`; it is a dry run unless
  asked to apply, and file I/O stays in `store` (the architecture test covers `service`).
- `service.Audit` (`trader data audit`, ADR-090 section 9) composes `LoadSeries` (months,
  gaps) and `VerifyCanonical` into one `AuditReport` that marshals to JSON; it adds no
  rule. The command only chooses the format and exits non-zero on `ErrAuditFailed`.
- `Load` also checks the interior of the series with the market calendar
  (`canonical.ClosureAt` and `Gaps`, ADR-090 section 7): the weekly close is not a
  gap, a year-end holiday is a reported `KnownClosure`, a run on the reviewed list
  `canonical/known_holes.csv` (bars the provider does not have) is a reported
  `KnownHole`, any other missing bar is `Unexplained` and fails the load unless `AllowMissing`. The ends of the range are
  not judged (a forming candle is never stored). `Load` does not call
  `Series.Validate`: every file is validated as it is read and months are read in
  order, so the series is already valid; a consumer that needs the guarantee
  (`runSeries`) validates it itself.
- `Bar.Instrument` names a bar. Readers set it from the file's key; it is never a
  column of a row.

## The engine

The root package runs on `marketdata.Bar` and replays `marketdata.Series` values
that a reader (`Load`) has already loaded; it has no series type of its own and
opens no file, resolves no path and knows no provider. Prices are read through
`Bar.OHLC()` (mid first). The scenario files in `testdata/emax` are read into a
`Series` (source `synthetic`) as mid.

`backtest.runSeries` replays one or more series on one trader with one shared
account and broker (`replay.go`, `backtest.go`):

- A run has one interval; series of different intervals are refused. Series are of
  different instruments, and an empty series contributes nothing.
- `mergedBars` orders bars by their stored `AvailableAt`, then by instrument name,
  so the order does not depend on the order of the series. `AvailableAt` is never
  recomputed from the opening time or the interval, and an opening time never
  authorises early delivery.
- `frames` groups the bars with the same `AvailableAt`: one group per distinct
  instant, holding only fresh bars. An instrument with no bar at that instant is
  absent; nothing is forward-filled or reused.
- Per group the trader advances the broker through the whole group, then calls
  `strategy.onBars(ctx, bars)` once with the whole fresh group (instrument order).
  The strategy sees every bar of the group before any of its orders is submitted;
  the trader then submits the returned orders in order. `eachBar` adapts a decision
  made one bar at a time (`emax`, `emaxEach`). A single-instrument run is the same
  path with one-bar groups. `backtest.run` keeps the per-bar access the research
  tests and studies use.
- `backtest.runSeries` validates every series (`marketdata.Series.Validate`) before
  replaying; `mergedBars` trusts its input and checks only what the merge needs.
- `trader.RunSeries(ctx, name, series...)` is the engine's entry for series a reader
  loaded: it works on copies, normalizes instrument keys, refuses an instrument it
  has no market for, runs EMAX on each instrument with the scenarios' fixed
  settings and returns a `Result` (`Write` prints the summary). The engine opens no
  file. `service.RunBacktest` is the application service that joins the two:
  `LoadSeries` (the market-data service's policy for missing, bad and stale
  months), then `RunSeries`; `trader backtest` only parses flags and prints.
- Mid prices and ticks (#667). A canonical mid is the exact midpoint of bid and
  ask, so it can sit half a tick from a whole tick (1.100005 for ticks of 0.00001).
  `trader.RunSeries` rounds every bar price to the nearest tick of the market (a
  tie goes up) as the bars enter the engine (`catalog.roundToTicks`), on its own
  copy: the canonical data and the series passed in are unchanged. The series is
  validated as it was given first; the engine's bar then carries only the price
  series it trades (`Bar.OHLC`, as `Mid`), since a rounded mid is no longer the
  midpoint of the stored bid and ask. The engine and its strategies only see whole
  ticks. The scenario path does not round: it
  still refuses a bar that is off the tick.
- Warm-up. `trader.RunSeries` takes `tradeFrom`: a bar that opens before it is a
  warm-up bar, shown to a strategy that implements `warmer` only to prefill its
  indicators (EMAX feeds its EMAs, takes no position, places no order), never
  replayed on the broker and not counted. After the warm-up EMAX finds its EMAs
  seeded, so it trades the trend as it stands. `service.RunBacktest` supplies the
  bars of the UTC month before `From` when they can be loaded: it uses the loader
  and the run's `AllowBad`, treats a missing month as simply absent, and runs an
  instrument without warm-up (logged) when its earlier months are stale or bad.
  Only the traded range is strict.

## Tests

- Every new or changed function has tests, including error and boundary cases.
- One test file per source file, named after it; cross-file tests and shared helpers
  in `<package>_test.go` (rules in [ai-skills.org](workflows/ai-skills.org)).
- Tests never touch `/srv/data`: each package that uses the store has
  `func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }`, and tests that
  write call `storetest.Root(t)` for a fresh temporary root.
- Fixtures: shared ones in the root `testdata/`, package ones in the package's
  `testdata/` ([testing architecture](arch/testing.md)).
- `make check` before every pull request, and the EMAX summaries unchanged
  (`go run ./cmd/trader 2>/dev/null | md5sum` prints
  `195e457a21def1223e7181c86cb824a4`).

## Adding things

- **A command:** a file in `cmd/trader` that resolves options and calls a service;
  import nothing but the service, the store and logging, and add the import-guard
  test (see `build_test.go`).
- **A provider capability:** in `provider/<name>`; map to the public model and add
  nothing domain-specific. Wire it in an application service.
- **A domain rule:** in the domain service, with a test per rule.
- **A file format or path:** a new version name and a rebuild, never a silent
  change; paths come from `layout`.
- **An ADR:** one decision per record; do not edit an accepted ADR silently, add an
  amendment or a new record.
