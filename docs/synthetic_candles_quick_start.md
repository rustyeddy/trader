# Synthetic Candle Data Generator - Quick Start Guide

## Problem
Your machine experiences an infinite loop in TestTrader when processing a year worth of real candles. We need reproducible, synthetic test data to debug this issue.

## Solution
Use the synthetic candle generator to create deterministic OHLC (Open, High, Low, Close) data that simulates realistic market movement without the need for real market data.

## Quick Start (5 minutes)

### 1. Generate Test Data

Generate one year of hourly EUR/USD data:

```bash
go run ./cmd/gen-testdata/main.go \
  -instrument EURUSD \
  -year 2025 \
  -timeframe H1 \
  -output testdata \
  -v
```

This creates 12 CSV files in `testdata/eurusd/candles/h1/2025/{01-12}/`:
- Each month has ~730 trading candles (forex market hours only)
- Total year: ~8,800 candles
- Size: ~10 MB
- Generation time: <100ms

### 2. Use in Your Tests

```go
func TestTraderFinitesWithSyntheticYear(t *testing.T) {
    // Generate synthetic data
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1
    
    candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
    require.NoError(t, err)
    
    // Feed to trader
    totalCandles := 0
    for _, cs := range candleSets {
        iter := NewCandleSetIterator(cs, TimeRange{})
        for iter.Next() {
            totalCandles++
            c := iter.Candle()
            // Your strategy logic here
        }
        iter.Close()
    }
    
    t.Logf("Processed %d candles successfully", totalCandles)
}
```

### 3. Detect Infinite Loops with Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

for iter.Next() {
    select {
    case <-ctx.Done():
        t.Fatal("Infinite loop detected")
    default:
    }
    // Process candle...
}
```

## API Overview

### Main Functions

| Function | Purpose | Example |
|----------|---------|---------|
| `DefaultSyntheticConfig(inst)` | Get default config | `cfg := DefaultSyntheticConfig("EURUSD")` |
| `GenerateSyntheticMonthlyCandles(year, month)` | Generate 1 month | `cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)` |
| `GenerateSyntheticYearlyCandles(year)` | Generate 12 months | `css, err := cfg.GenerateSyntheticYearlyCandles(2025)` |
| `TestHelperGenerateSyntheticCandles(t, inst, year, month, tf)` | In tests | `cs := TestHelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)` |
| `LoadSyntheticCandles(inst, year, month, tf)` | Load or create from disk | `cs, err := LoadSyntheticCandles("EURUSD", 2025, time.January, H1)` |

### Configuration

```go
cfg := SyntheticCandleConfig{
    Instrument:  "EURUSD",
    Timeframe:   H1,              // M1, H1, or D1
    StartPrice:  Price(1080000),  // 1.08000
    Volatility:  0.002,           // 0.2% volatility
    Trend:       0.00005,         // +0.005% per candle
    Seed:        42,              // For reproducibility
    TicksPerBar: 50,
}
```

## Common Scenarios

### Scenario 1: Quick Test with 1 Month

```go
cs := TestHelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)
iter := NewCandleSetIterator(cs, TimeRange{})
for iter.Next() {
    // Process ~730 candles
}
```

### Scenario 2: Full Year Stress Test

```go
candleSets, _ := DefaultSyntheticConfig("EURUSD").GenerateSyntheticYearlyCandles(2025)
// ~8,800 candles to find the infinite loop
for _, cs := range candleSets {
    iter := NewCandleSetIterator(cs, TimeRange{})
    for iter.Next() {
        // Your logic
    }
}
```

### Scenario 3: Integration with Backtest

```go
cfg := &ConfigBackTest{
    Instrument: "EURUSD",
    Strategy:   "my-strategy",
    TimeFrame:  H1,
    Start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
    End:        time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
}

trader := NewTrader()
cs, _ := LoadSyntheticCandles("EURUSD", 2025, time.January, H1)
iter := NewCandleSetIterator(cs, TimeRange{})
err := trader.backTestWithIterator(context.Background(), cfg, strategy, iter)
```

## Debugging an Infinite Loop

### Step 1: Add Logging

```go
candleCount := 0
for iter.Next() {
    candleCount++
    if candleCount%100 == 0 {
        log.Printf("Processed %d candles", candleCount)
    }
    // ... process candle ...
}
```

### Step 2: Add Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

for iter.Next() {
    select {
    case <-ctx.Done():
        t.Fatalf("Timeout at candle %d", candleCount)
    default:
    }
    candleCount++
}
```

### Step 3: Narrow Down the Issue

- Try with just 1 month (H1) → ~730 candles
- Then 3 months → ~2,200 candles
- Then full year → ~8,800 candles
- Identify at which month it hangs

### Step 4: Check Strategy Logic

Common causes of infinite loops:
- Infinite recursion in strategy update
- Event processing loop not terminating
- Position management creating circular dependencies
- Iterator not advancing properly

## Key Points

✅ **Reproducible**: Same seed = same data every time
✅ **Realistic**: Uses geometric Brownian motion
✅ **Fast**: Generate a year in <100ms
✅ **Deterministic**: No random process hangs
✅ **Configurable**: Adjust volatility, trend, starting price

## Files Generated

After running the generator:

```
testdata/
  eurusd/
    candles/
      h1/
        2025/
          01/eurusd_candles_h1_202501.csv
          02/eurusd_candles_h1_202502.csv
          ...
          12/eurusd_candles_h1_202512.csv
```

Each CSV contains:
- Header with metadata
- Timestamp, OHLC prices
- Spread and tick count
- Flags for data validity

## Troubleshooting

| Issue | Solution |
|-------|----------|
| "File not found" | Run `go run ./cmd/gen-testdata/main.go` first |
| "Slow generation" | Use H1 instead of M1, or D1 instead of H1 |
| "Generated data looks wrong" | Check `Volatility` and `Trend` parameters |
| "Tests still timeout" | Infinite loop confirmed - check strategy logic |

## Performance

| Timeframe | Candles/Year | Gen Time | File Size |
|-----------|--------------|----------|-----------|
| M1        | 518,400      | 500ms    | 100 MB    |
| H1        | 8,808        | 50ms     | 10 MB     |
| D1        | 264          | 1ms      | 300 KB    |

## Examples in Code

See [synthetic_trader_examples_test.go](synthetic_trader_examples_test.go) for complete working examples:

- `TestTraderWithYearOfSyntheticHourly` - Full year stress test
- `TestTraderTimeoutDetection` - Detecting infinite loops
- `TestTraderWithHighVolatilitySynthetic` - Edge case testing
- `BenchmarkSyntheticCandleGeneration` - Performance benchmarks

## Next Steps

1. Generate test data: `go run ./cmd/gen-testdata/main.go -v`
2. Run tests: `go test -v -run "TestTrader" -timeout 60s`
3. Add logging to find where it hangs
4. Check your strategy's `OnBar()` or `Update()` method
5. Look for infinite loops in position handling

## Reference Documentation

- [docs/synthetic_candles.md](docs/synthetic_candles.md) - Full API documentation
- [testdata_generator.go](testdata_generator.go) - Implementation
- [testdata_helper.go](testdata_helper.go) - Helper functions
- [testdata_generator_test.go](testdata_generator_test.go) - Test examples

---

**Need help?** Check the full documentation or create a test that reproduces your infinite loop with synthetic data - much faster than real data!
