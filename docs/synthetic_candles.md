# Synthetic Candle Data Generator

## Overview

This synthetic candle data generator creates reproducible, realistic OHLC (Open, High, Low, Close) market data for testing the Trader application. It's especially useful for debugging issues like infinite loops when processing large datasets with real market data.

## Why Use Synthetic Data?

- **Reproducible**: Same seed produces identical candles on every run
- **Deterministic**: No random network failures or data inconsistencies
- **Controllable**: Adjust volatility, trends, and patterns directly
- **Fast**: Generate a year's worth of candles in milliseconds
- **Realistic**: Uses geometric Brownian motion to produce realistic price movement

## Quick Start

### Generate Test Data

```bash
# Generate 1 year of hourly EUR/USD data in testdata/
go run ./cmd/gen-testdata/main.go -instrument EURUSD -year 2025 -timeframe H1 -output testdata

# With verbose output:
go run ./cmd/gen-testdata/main.go -instrument EURUSD -year 2025 -timeframe H1 -output testdata -v
```

### Use in Tests

```go
// In your test file
func TestTraderWithSyntheticData(t *testing.T) {
    // Generate a month of H1 candles
    cs := TestHelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)
    
    // Convert to iterator and feed to trader
    iter := NewCandleSetIterator(cs, TimeRange{})
    defer iter.Close()
    
    // Feed candles to your strategy/trader
    for iter.Next() {
        candle := iter.Candle()
        timestamp := iter.Timestamp()
        
        // Process candle...
    }
}
```

## API Reference

### Generating Synthetic Candles

#### DefaultSyntheticConfig(instrument string) SyntheticCandleConfig
Returns a sensible default configuration:
- EUR/USD at 1.08000
- 0.2% volatility (typical for major pairs)
- +0.005% uptrend per candle
- 50 ticks per candle

```go
cfg := DefaultSyntheticConfig("EURUSD")
```

#### Custom Configuration
```go
cfg := SyntheticCandleConfig{
    Instrument:  "GBPUSD",
    Timeframe:   H1,
    StartPrice:  Price(1250000),  // 1.25000
    Volatility:  0.0015,           // 0.15% volatility
    Trend:       0.00003,          // +0.003% per candle
    Seed:        42,
    TicksPerBar: 100,
}
```

#### Generate Monthly Data
```go
cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
if err != nil {
    panic(err)
}
```

#### Generate Yearly Data
```go
candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
if err != nil {
    panic(err)
}
// candleSets is []*CandleSet with 12 months
```

#### Write to CSV Files
```go
// Write monthly data to testdata directory
paths, err := cfg.GenerateSyntheticYearlyAndWrite(&Store{basedir: "testdata"}, 2025)
if err != nil {
    panic(err)
}
// paths contains file paths for all 12 months
```

### Helper Functions

#### TestHelperGenerateSyntheticCandles
Generate synthetic candles in tests without boilerplate:
```go
cs := TestHelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)
```

#### TestHelperGenerateSyntheticCandlesWithConfig
Generate with custom config:
```go
cfg := SyntheticCandleConfig{...}
cs := TestHelperGenerateSyntheticCandlesWithConfig(t, cfg, 2025, time.January)
```

#### MakeSyntheticCandleSetIterator
Convert CandleSet to iterator:
```go
iter := MakeSyntheticCandleSetIterator(cs)
for iter.Next() {
    c := iter.Candle()
    ts := iter.Timestamp()
}
```

#### LoadSyntheticCandles
Load or create synthetic candles on-demand:
```go
cs, err := LoadSyntheticCandles("EURUSD", 2025, time.January, H1)
if err != nil {
    panic(err)
}
// Creates testdata files if they don't exist
```

## Debugging Infinite Loops

### Finding the Issue

The synthetic data helps you isolate the problem:

1. **Generate reproducible data**:
   ```go
   cfg := DefaultSyntheticConfig("EURUSD")
   cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
   ```

2. **Feed to trader with timeout**:
   ```go
   iter := NewCandleSetIterator(cs, TimeRange{})
   
   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
   defer cancel()
   
   for iter.Next() {
       select {
       case <-ctx.Done():
           t.Fatal("Infinite loop detected: processing took too long")
       default:
       }
       // Process candle
   }
   ```

3. **Add logging**:
   ```go
   candleCount := 0
   for iter.Next() {
       candleCount++
       if candleCount % 100 == 0 {
           t.Logf("Processed %d candles", candleCount)
       }
       // Process candle
   }
   ```

### Example Test

```go
func TestTraderFinitesWithYear(t *testing.T) {
    // Generate a full year of data with controlled parameters
    cfg := DefaultSyntheticConfig("EURUSD")
    cfg.Timeframe = H1  // Start with hourly (365 * 24 = ~8760 candles)
    cfg.Volatility = 0.002
    cfg.Seed = 42
    
    candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
    require.NoError(t, err)
    
    // Combine all months into one iterator
    totalCandles := 0
    for _, cs := range candleSets {
        iter := NewCandleSetIterator(cs, TimeRange{})
        defer iter.Close()
        
        for iter.Next() {
            totalCandles++
            c := iter.Candle()
            
            // Your strategy/trader logic here
            // If this loops infinitely, it will timeout
            
            if totalCandles > 100000 {
                t.Fatalf("Too many candles: infinite loop suspected")
            }
        }
    }
    
    // Should complete with finite number of candles
    t.Logf("Processed %d candles successfully", totalCandles)
}
```

## Configuration Options

### Volatility
- `0.0005` = very calm (stable pairs)
- `0.002` = typical for major forex pairs
- `0.01` = very volatile (exotic pairs)

### Trend
- `0.00` = random walk (no trend)
- `0.0001` = +0.01% per candle (~2.5% per day for H1)
- `-0.0001` = -0.01% per candle (downtrend)

### Seed
Use consistent seeds for reproducibility:
```go
// Same seed = same candle sequence
cfg1.Seed = 42
cfg2.Seed = 42
// cs1 and cs2 will have identical OHLC data
```

### Timeframe
Supported timeframes:
- `M1` = 1 minute
- `H1` = 1 hour (default, ~730 candles per month)
- `D1` = 1 day (~22 candles per month)

## Performance Characteristics

| Timeframe | Candles/Month | Candles/Year | Gen Time |
|-----------|---------------|--------------|----------|
| M1        | ~43,200       | ~518,400     | ~500ms   |
| H1        | ~734          | ~8,808       | ~50ms    |
| D1        | ~22           | ~264         | ~1ms     |

*Times are approximate on modern hardware*

## File Format

Synthetic candles are stored as CSV in a directory structure:

```
testdata/
  {instrument}/
    candles/
      {timeframe}/
        {year}/
          {month}/
            {instrument}_candles_{timeframe}_{year}{month}.csv
```

Example:
```
testdata/eurusd/candles/h1/2025/01/eurusd_candles_h1_202501.csv
```

Each CSV contains:
```
# Generated synthetic candles for EURUSD H1 2025-01-01
# Timeframe: 3600 seconds
Timestamp,High,Open,Low,Close,AvgSpread,MaxSpread,Ticks,Flags
1735686000,101000,100000,99000,100500,2,5,50,0x0001
1735689600,101500,100500,100000,101000,2,5,50,0x0001
...
```

## Advanced Usage

### Generating Multiple Pairs

```go
instruments := []string{"EURUSD", "GBPUSD", "USDJPY"}
for _, inst := range instruments {
    paths, err := GenerateSyntheticYearTestData("testdata", inst, 2025, H1)
    if err != nil {
        log.Printf("Failed to generate %s: %v", inst, err)
    }
}
```

### Creating Custom Scenarios

```go
// Simulate a crash
crashCfg := DefaultSyntheticConfig("EURUSD")
crashCfg.Trend = -0.001      // -0.1% per candle
crashCfg.Volatility = 0.01   // High volatility during crash

// Simulate recovery
recoveryCfg := DefaultSyntheticConfig("EURUSD")
recoveryCfg.Trend = 0.0005   // Recovery rally
recoveryCfg.Volatility = 0.005 // Elevated vol but not panicked

crash, _ := crashCfg.GenerateSyntheticMonthlyCandles(2025, time.January)
recovery, _ := recoveryCfg.GenerateSyntheticMonthlyCandles(2025, time.February)
```

### Integration with Backtest

```go
func TestBacktestOnSyntheticData(t *testing.T) {
    cs, _ := LoadSyntheticCandles("EURUSD", 2025, time.January, H1)
    
    cfg := &ConfigBackTest{
        Instrument: "EURUSD",
        Strategy:   "ema-cross",
        TimeFrame:  H1,
        Start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
        End:        time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
    }
    
    trader := newTestTrader()
    itr := NewCandleSetIterator(cs, TimeRange{})
    err := trader.backTestWithIterator(context.Background(), cfg, strategy, itr)
    require.NoError(t, err)
}
```

## Troubleshooting

### Generated Data Looks Unrealistic
- Adjust volatility (typically 0.001-0.005 for major pairs)
- Check the trend isn't too extremeCheck the starting price

### Files Not Being Created
- Verify output directory is writable
- Check disk space
- Ensure Store.basedir path is correct

### Same Seed Producing Different Data
- Seed alone doesn't guarantee reproduction across versions
- The LCG algorithm is intentionally simple
- Use exact same code for reproducibility guarantees

## See Also

- [Store](store.go) - CSV I/O for candle data
- [Types Candle](types_candle.go) - CandleSet structure
- [Iterator](iterator.go) - Candle iteration utilities
- [Test Examples](testdata_generator_test.go) - Usage examples
