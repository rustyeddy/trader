# Trader

A professional-grade FX trading simulator and research platform written in Go.

## Features
- Risk-based position sizing
- FX-correct P/L accounting
- Stop-loss / take-profit enforcement
- Margin usage & forced liquidation
- Paper trading engine
- Trade journal (CSV / SQLite)
- Equity curve tracking
- OANDA API integration for historic candle data

## Supported Instruments
- EUR_USD
- USD_JPY

## Quick Start

```bash
# Build the CLI
make build

# Run a demo
./bin/trader demo basic

# Run a simple simulation
./bin/trader run -config examples/configs/basic.yaml

# Try backtesting
./bin/trader backtest -ticks examples/data/sample_ticks.csv -strategy noop

# See all commands
./bin/trader --help
```

**New to the project?** See [GETTING_STARTED.md](docs/GETTING_STARTED.md) for a comprehensive guide.

## Documentation

- **[Getting Started Guide](docs/GETTING_STARTED.md)** - Installation, first steps, and core concepts
- **[Architecture Overview](docs/ARCHITECTURE.md)** - System design and component details
- **[Examples](examples/)** - Sample trading strategies and use cases
- **[Contributing Guide](docs/CONTRIBUTING.md)** - How to contribute to the project

## CLI Commands

The `trader` CLI provides a comprehensive set of commands:

### Core Commands
- **run** - Run simulations from config files
- **backtest** - Test trading strategies with historical data
- **replay** - Replay tick data from CSV files
- **demo** - Run example simulations (basic, risk, simrun)

### Utility Commands
- **config** - Generate or validate configuration files
- **journal** - Query trade journal data
- **oa2csv** - Download OANDA candle data to CSV

### Examples

```bash
# Run a simulation
./bin/trader run -config examples/configs/basic.yaml

# Backtest an EMA crossover strategy
./bin/trader backtest -ticks data/eurusd.csv -strategy ema-cross -fast 20 -slow 50

# Replay historical data
./bin/trader replay -ticks examples/data/sample_ticks.csv

# Run demos to learn the system
./bin/trader demo basic
./bin/trader demo risk

# Manage configurations
./bin/trader config init -output my-config.yaml
./bin/trader config validate -config my-config.yaml

# Query trade journal
./bin/trader journal today
./bin/trader journal trade <trade-id>

# Download OANDA data
./bin/trader oa2csv -token YOUR_TOKEN -instrument EUR_USD \
  -from 2024-01-01T00:00:00Z -to 2025-01-01T00:00:00Z
```

## Examples

Explore practical examples in the `examples/` directory:

- **basic/** - Simple single trade with stop loss and take profit
- **multiple/** - Managing multiple positions simultaneously  
- **risk/** - Demonstrates proper position sizing
- **oanda/** - Download historic candles from OANDA account
- **simrun/** - Simple simulation runner

You can run these as standalone programs or through the CLI:

```bash
# Run demos via CLI
./bin/trader demo basic
./bin/trader demo risk
./bin/trader demo simrun

# Or run examples directly
go run ./examples/basic/main.go
go run ./examples/oanda/main.go  # Requires OANDA_TOKEN env var
```

## Building

```bash
# Build the CLI
make build

# The binary will be at bin/trader
./bin/trader --help

# Run tests
make test

# Generate coverage report
make cover
```

## Migration from Previous CLI

If you were using the old separate CLI binaries, here's the migration guide:

**Old commands** → **New commands**
- `./cmd/backtest/backtest -ticks data.csv` → `trader backtest -t data.csv`
- `./cmd/oa2csv/oa2csv -token TOKEN` → `trader oa2csv --token TOKEN`
- `./cmd/replay/replay -ticks data.csv` → `trader replay -t data.csv`
- `./cmd/trader run -config cfg.yaml` → `trader run -f cfg.yaml`
- `go run ./examples/basic/main.go` → `trader demo basic`

See [CLI_ARCHITECTURE.md](docs/CLI_ARCHITECTURE.md) for complete CLI documentation.

Core invariants (non-negotiable)

These should always hold:

Accounting

Equity = Balance + UnrealizedPL

FreeMargin = Equity − MarginUsed

Equity never jumps without:

price movement, or

trade open/close

P/L

P/L calculated in quote currency

Converted once → account currency

BUY uses bid to close

SELL uses ask to close

Stops

SL/TP evaluated on every price update

Stop price is inclusive

Close price = triggering bid/ask (not stop price)

Margin

Margin uses mid price

Margin recomputed after every close

Forced liquidation never leaves Equity < MarginUsed

Journaling

Every trade closed exactly once

Every equity snapshot monotonic in time

Journal writes never affect engine state

