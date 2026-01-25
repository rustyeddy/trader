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

## Supported Instruments
- EUR_USD
- USD_JPY

## Quick Start

```bash
# Run a simple simulation
go run ./cmd/simrun

# Try the examples
go run ./examples/basic_trade.go
go run ./examples/risk_management.go
```

**New to the project?** See [GETTING_STARTED.md](docs/GETTING_STARTED.md) for a comprehensive guide.

## Documentation

- **[Getting Started Guide](docs/GETTING_STARTED.md)** - Installation, first steps, and core concepts
- **[Architecture Overview](docs/ARCHITECTURE.md)** - System design and component details
- **[Examples](examples/)** - Sample trading strategies and use cases
- **[Contributing Guide](docs/CONTRIBUTING.md)** - How to contribute to the project

## Examples

Explore practical examples in the `examples/` directory:

- **basic_trade.go** - Simple single trade with stop loss and take profit
- **multiple_trades.go** - Managing multiple positions simultaneously  
- **risk_management.go** - Demonstrates proper position sizing

```bash
go run ./examples/basic_trade.go
```

## Building

```bash
# Run tests
make test

# Build the CLI tools
make build

# Generate coverage report
make cover
```

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

