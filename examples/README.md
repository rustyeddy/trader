# Trading Examples

This directory contains runnable examples aligned with the current codebase.

## Available Examples

### Go programs

- **[basic/](basic/)** - Minimal single-order flow against the in-memory simulator
- **[multiple/](multiple/)** - Open positions across multiple instruments
- **[simrun/](simrun/)** - Minimal multi-tick simulation run

### Config-based examples

- **[configs/backtest_fake_eurusd_2023.yml](configs/backtest_fake_eurusd_2023.yml)** - Batch backtest config for the fake strategy
- **[data/sample_ticks.csv](data/sample_ticks.csv)** - Sample input for `replay pricing`

## Running Examples

```bash
go run ./examples/basic
go run ./examples/multiple
go run ./examples/simrun
```

```bash
# Build CLI
make build

# Replay sample price ticks
./bin/trader replay pricing --ticks ./examples/data/sample_ticks.csv

# Run fake-strategy batch backtest (requires candle data available locally)
./bin/trader --config ./examples/configs/backtest_fake_eurusd_2023.yml backtest
```

## Useful examples to add next

- **EMA cross backtest config** for the `backtest ema-cross` command
- **EMA cross + ADX backtest config** for the `backtest ema-cross-adx` command
- **SQLite journal query workflow** example (`journal trade/day/today` commands)
