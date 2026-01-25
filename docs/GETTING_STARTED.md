# Getting Started with Trader

This guide will help you get up and running with the Trader FX simulation platform.

## Prerequisites

- Go 1.25 or later
- Git
- Basic understanding of FX trading concepts

## Installation

1. Clone the repository:
```bash
git clone https://github.com/rustyeddy/trader.git
cd trader
```

2. Install dependencies:
```bash
go mod download
```

3. Run tests to verify installation:
```bash
make test
```

## Quick Start

### Running Your First Simulation

The simplest way to get started is to run the included sample simulation:

```bash
go run ./cmd/simrun
```

This will:
- Initialize a simulated account with $100,000 USD
- Execute a sample EUR/USD trade with risk-based position sizing
- Output trade results to `trades.csv` and `equity.csv`

### Understanding the Output

After running the simulation, check the generated CSV files:

**trades.csv** - Contains closed trade records:
- `trade_id`: Unique identifier for each trade
- `instrument`: Currency pair (e.g., EUR_USD)
- `units`: Position size (positive = BUY, negative = SELL)
- `entry_price`: Price at which the trade was opened
- `exit_price`: Price at which the trade was closed
- `realized_pl`: Profit/loss in account currency
- `reason`: Why the trade closed (StopLoss, TakeProfit, LIQUIDATION)

**equity.csv** - Contains account snapshots:
- `time`: Timestamp of the snapshot
- `balance`: Total realized balance
- `equity`: Balance + unrealized P/L
- `margin_used`: Margin required for open positions
- `free_margin`: Available margin for new trades

## Building the CLI Tools

The project includes command-line tools for simulations, replay, and journal querying:

```bash
# Build the trader CLI
go build -o trader ./cmd/trader

# Or use make
make build
```

### Using the Trader CLI

The trader CLI provides several commands:

#### Running Simulations

```bash
# Run a simulation from a config file
./trader run -config examples/configs/basic.yaml
```

#### Replaying Historical Data

The replay command allows you to replay historical tick data from CSV files:

```bash
# Direct CSV replay with default settings
./trader replay -ticks data/ticks.csv -db replay.db

# Replay from a configuration file
./trader replay -config replay-config.yaml
```

CSV format for tick data:
```csv
time,instrument,bid,ask,event,arg1,arg2,arg3,arg4
2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002,OPEN,EUR_USD,10000
2026-01-24T09:30:05Z,EUR_USD,1.1010,1.1012,,,
2026-01-24T09:30:10Z,EUR_USD,1.1020,1.1022,CLOSE_ALL,EndOfDay
```

Supported events:
- `OPEN`: Open a market order (args: instrument, units)
- `OPEN_SLTP`: Open with stop loss and take profit (args: instrument, units, stopLoss, takeProfit)
- `CLOSE`: Close a specific trade (args: tradeID, reason)
- `CLOSE_ALL`: Close all open trades (args: reason)

Configuration-based replay example:
```yaml
account:
  id: "REPLAY-001"
  currency: "USD"
  balance: 100000

journal:
  type: "sqlite"
  db_path: "./replay.db"

replay:
  csv_file: "./data/ticks.csv"
  tick_then_event: true
  close_at_end: true
```

#### Querying the Journal

```bash
# View a specific trade
go run ./cmd/trader journal -db ./trader.sqlite trade <trade_id>

# View today's trades
go run ./cmd/trader journal -db ./trader.sqlite today

# View trades for a specific day
go run ./cmd/trader journal -db ./trader.sqlite day 2026-01-24
```

## Core Concepts

### Account & Equity

Your account tracks:
- **Balance**: Realized P/L from closed trades
- **Equity**: Balance + unrealized P/L from open trades
- **Margin Used**: Capital locked for open positions
- **Free Margin**: Capital available for new trades

The core invariant: `Equity = Balance + UnrealizedPL`

### Risk-Based Position Sizing

The platform uses professional risk management to calculate position sizes:

```go
size := risk.Calculate(risk.Inputs{
    Equity:         100_000,      // Account equity
    RiskPct:        0.005,         // Risk 0.5% per trade
    EntryPrice:     1.0851,        // Entry price
    StopPrice:      1.0831,        // Stop loss (20 pips away)
    PipLocation:    -4,            // For EUR/USD
    QuoteToAccount: 1.0,           // USD quote = 1.0 conversion
})
```

This ensures each trade risks exactly 0.5% of your equity, regardless of stop distance.

### Price-Time Priority

All trades execute at realistic prices:
- **BUY orders**: Open at ask price, close at bid price
- **SELL orders**: Open at bid price, close at ask price
- Spread costs are always accounted for

### Stop Loss & Take Profit

Stops are evaluated on every price tick:
- Stop prices are inclusive (triggers when price reaches stop)
- Close price is the triggering bid/ask (not the stop price itself)
- Both SL and TP are optional but recommended for risk management

### Margin & Liquidation

The platform enforces margin requirements:
- Each position uses 2% of notional value as margin
- If `Equity < MarginUsed`, worst-performing trades are liquidated
- Liquidation continues until margin requirements are satisfied

## Next Steps

1. **Read the Architecture**: See [ARCHITECTURE.md](ARCHITECTURE.md) for system design
2. **Explore Examples**: Check the `examples/` directory for sample strategies
3. **Write Your Strategy**: Create custom trading logic in the `strategy/` package
4. **Contribute**: See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines

## Common Issues

### Module Not Found Errors

If you see import errors, ensure you're in the project root and run:
```bash
go mod tidy
```

### CSV Files Not Created

The simulation creates CSV files in the current directory. Ensure:
- You have write permissions in the directory
- The journal is properly initialized in your code
- The simulation actually executes trades (check for errors)

### Test Failures

If tests fail after changes:
```bash
# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test -v ./sim

# View test coverage
make cover
```

## Getting Help

- Review the [README](../README.md) for feature overview
- Check existing [Issues](https://github.com/rustyeddy/trader/issues) on GitHub
- Open a new issue for bugs or feature requests
