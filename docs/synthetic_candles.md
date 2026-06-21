# Synthetic Candle Data

Trader includes a deterministic synthetic OHLC generator for tests,
reproducible fixtures, iterator stress checks, and strategy scenarios that do
not need historical market data.

Synthetic candles are test data, not a market simulator or a substitute for
historical-data regression tests. The generator uses a simple deterministic
pseudo-random sequence and geometric price changes; it does not model order
books, volatility regimes, correlated instruments, or broker execution.

## Current implementation

| File | Purpose |
|---|---|
| `testdata_generator.go` | Generator configuration, deterministic RNG, monthly/yearly generation, and store writes |
| `testdata_helper.go` | Public helper for writing a synthetic year to a chosen store root |
| `testdata_helper_test.go` | Helpers available only to tests in package `trader` |
| `testdata_generator_test.go` | Generator, determinism, file, iterator, and integration tests |
| `data_synthetic_trader_test.go` | Full-year traversal stress tests and benchmarks |
| `cmd/gen-testdata/main.go` | Standalone fixture-generation command |

The original implementation-summary and quick-start documents were removed
after their APIs, filenames, performance claims, and debugging narrative
became stale. This file is the authoritative guide.

## Generator model

`SyntheticCandleConfig` controls generation:

```go
type SyntheticCandleConfig struct {
    Instrument  string
    Timeframe   Timeframe
    StartPrice  Price
    Volatility  float64
    Trend       float64
    Seed        int64
    TicksPerBar int32
}
```

`DefaultSyntheticConfig("EURUSD")` currently selects:

| Field | Default |
|---|---|
| Instrument | Caller-supplied value |
| Timeframe | `H1` |
| Start price | `Price(108000)`, representing 1.08000 at `PriceScale=100000` |
| Volatility | `0.002` |
| Trend | `0.00005` per candle |
| Seed | zero, normalized by the RNG to its deterministic fallback |
| Ticks per bar | `50` |

The generator:

1. allocates one canonical monthly candle set;
2. derives a deterministic month seed from the configured seed, year, and
   month;
3. walks every timeframe slot in that month;
4. leaves closed-market slots invalid;
5. generates OHLC, spread, and tick values for open-market slots;
6. uses each generated close as the next open.

The same configuration, year, and month produce the same sequence for the
same implementation version. Generated data may change when the algorithm is
intentionally revised, so long-lived expected results should be committed as
fixtures rather than regenerated implicitly.

The current synthetic generator uses floating-point stochastic calculations
internally before converting results to `Price`. It is test tooling, but this
still falls under the fixed-point migration tracked by
[GitHub issue #148](https://github.com/rustyeddy/trader/issues/148).

## Generate fixture files

Generate twelve monthly files with the standalone command:

```bash
go run ./cmd/gen-testdata \
  -instrument EURUSD \
  -year 2025 \
  -timeframe H1 \
  -output /tmp/trader-synthetic \
  -v
```

Supported command timeframes are `M1`, `H1`, and `D1`. The default output root
is `testdata`, but routine tests and experiments should use a temporary
directory to avoid modifying committed fixtures.

Files use the current canonical store layout:

```text
<output>/
  synthetic/
    EURUSD/
      2025/
        01/
          EURUSD-2025-01-h1.csv
        ...
        12/
          EURUSD-2025-12-h1.csv
```

The exact number of valid candles and file size depend on timeframe, month,
market-hours logic, and CSV implementation. Do not assert documentation-era
performance numbers or assume every allocated slot is valid.

## Generate data from Go

### Monthly in-memory data

The low-level monthly API returns the package-private `*candleSet` type. It is
therefore intended primarily for code and tests in package `trader`.

```go
cfg := DefaultSyntheticConfig("EURUSD")
cfg.Timeframe = H1
cfg.Seed = 42

cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
if err != nil {
    t.Fatal(err)
}

iter := newCandleSetIterator(cs, TimeRange{})
defer iter.Close()

for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
    _ = ct.Timestamp
    _ = ct.Candle
}
if err := iter.Err(); err != nil {
    t.Fatal(err)
}
```

Only valid market slots are returned by the iterator.

### Yearly in-memory data

```go
cfg := DefaultSyntheticConfig("EURUSD")
cfg.Timeframe = D1
cfg.Seed = 2025

months, err := cfg.GenerateSyntheticYearlyCandles(2025)
if err != nil {
    t.Fatal(err)
}
if len(months) != 12 {
    t.Fatalf("got %d months", len(months))
}
```

### Write a year to a chosen directory

`GenerateSyntheticYearTestData` is the simplest API for callers that only
need canonical CSV fixtures:

```go
paths, err := GenerateSyntheticYearTestData(
    t.TempDir(),
    "EURUSD",
    2025,
    H1,
)
if err != nil {
    t.Fatal(err)
}
```

It returns the twelve written paths. Passing an empty directory writes under
the relative `testdata` directory and should be avoided in ordinary tests.

## Package-local test helpers

`testdata_helper_test.go` defines helpers for tests declared with
`package trader`:

- `HelperGenerateSyntheticCandles`
- `HelperGenerateSyntheticCandlesWithConfig`
- `MakeSyntheticCandleSetIterator`
- `CreateTestDataFiles`
- `GetOrCreateTestData`
- `LoadSyntheticCandles`

Because these helpers live in a `_test.go` file and use the private
`candleSet`, they are not available to external packages or production code.
Tests in other packages should use their own small fixtures, a public store
API, or `GenerateSyntheticYearTestData`.

Prefer in-memory generation or `t.TempDir()` over helpers that write to the
repository's `testdata` directory.

## Useful test patterns

### Deterministic strategy input

```go
cfg := DefaultSyntheticConfig("EURUSD")
cfg.Timeframe = H1
cfg.Seed = 42

cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
require.NoError(t, err)

iter := newCandleSetIterator(cs, TimeRange{})
defer iter.Close()

for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
    plan := strategy.Update(context.Background(), &ct, run)
    // Assert exact signals or state transitions.
    _ = plan
}
require.NoError(t, iter.Err())
```

### Bounded traversal

Use Go's test timeout for suite-level deadlock protection:

```bash
go test ./... -run Synthetic -count=1 -timeout 30s
```

For a loop that also performs context-aware work, check a deadline inside the
loop:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
    if err := ctx.Err(); err != nil {
        t.Fatalf("synthetic traversal: %v", err)
    }
    _ = ct
}
```

### Edge scenarios

Adjust configuration explicitly to create stable stress inputs:

```go
cfg := DefaultSyntheticConfig("EURUSD")
cfg.Timeframe = H1
cfg.StartPrice = PriceFromFloat(1.08000)
cfg.Volatility = 0.01
cfg.Trend = -0.001
cfg.Seed = 12345
cfg.TicksPerBar = 200
```

These settings create a deterministic high-volatility downtrend, not a
statistically validated crash model.

## Running tests and benchmarks

Run focused generator and traversal tests:

```bash
go test ./... -run Synthetic -count=1
```

Run the root-package benchmarks:

```bash
go test . -run '^$' -bench 'Synthetic|YearGeneration' -benchmem
```

Benchmark numbers are machine- and version-dependent. Record them from the
current code when comparing changes instead of copying values from
documentation.

## Limitations and cautions

- Synthetic output is deterministic but not historically representative.
- Market-closed slots exist in the monthly backing set and are marked invalid.
- The low-level in-memory return type is package-private.
- Generation currently uses floating-point math.
- One seed does not provide broad scenario coverage; use multiple explicit
  seeds when that variation is material.
- A test that only iterates candles does not prove that the complete `Trader`
  backtest loop executed correctly.
- Do not generate fixtures into `/srv/trading/data` or overwrite committed
  `testdata` unintentionally.

For end-to-end engine behavior, use the normal backtest configuration and
`service.RunBacktest` with controlled candle-store fixtures.
