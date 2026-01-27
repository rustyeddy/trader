# Trader CLI Architecture

This document describes the consolidated Cobra-based CLI architecture for the trader project.

## Structure

The CLI is organized as a single binary (`trader`) with multiple subcommands. The code is located in `cmd/trader-cobra/`.

```
cmd/trader-cobra/
├── main.go                 # Entry point
└── cmd/
    ├── root.go            # Root command and shared functionality
    ├── backtest.go        # Backtesting command
    ├── emacross.go        # EMA crossover strategy implementation
    ├── oa2csv.go          # OANDA data download command
    ├── replay.go          # Replay command
    ├── run.go             # Simulation runner command
    ├── config.go          # Configuration management
    ├── journal.go         # Journal query command
    ├── demo.go            # Demo/example commands
    └── version.go         # Version command
```

## Commands

### Core Trading Commands

#### `trader run`
Runs a simulation from a configuration file.

**Usage:**
```bash
trader run -f config.yaml
```

**Flags:**
- `-f, --config` - Path to config file (YAML or JSON)

**Example:**
```bash
trader run -f examples/configs/basic.yaml
```

#### `trader backtest`
Runs backtests with various trading strategies against historical tick data.

**Usage:**
```bash
trader backtest -t ticks.csv -s strategy [options]
```

**Flags:**
- `-t, --ticks` - Path to tick CSV file (required)
- `-s, --strategy` - Strategy name: noop, open-once, ema-cross (default: noop)
- `-d, --db` - SQLite journal DB path (default: ./backtest.sqlite)
- `-b, --balance` - Starting account balance (default: 100000)
- `-i, --instrument` - Trading instrument (default: EUR_USD)
- `-u, --units` - Order units for some strategies (default: 10000)

**EMA Cross Strategy Flags:**
- `--fast` - Fast EMA period (default: 20)
- `--slow` - Slow EMA period (default: 50)
- `--risk` - Risk percent per trade (default: 0.005 = 0.5%)
- `--stop-pips` - Stop loss in pips (default: 20)
- `--rr` - Take profit as risk multiple (default: 2.0)

**Examples:**
```bash
# Noop strategy (baseline)
trader backtest -t data/eurusd.csv -s noop

# Open-once strategy
trader backtest -t data/eurusd.csv -s open-once -i EUR_USD -u 10000

# EMA crossover strategy
trader backtest -t data/eurusd.csv -s ema-cross --fast 20 --slow 50 --risk 0.01
```

#### `trader replay`
Replays historical tick data from CSV files.

**Usage:**
```bash
trader replay -t ticks.csv [options]
trader replay -f config.yaml
```

**Flags:**
- `-t, --ticks` - CSV file of ticks
- `-f, --config` - Configuration file with replay settings
- `-d, --db` - SQLite journal path (default: ./trader.sqlite)
- `--close-end` - Close all trades at end (default: true)

**Examples:**
```bash
# Direct CSV replay
trader replay -t examples/data/sample_ticks.csv

# Config-based replay
trader replay -f examples/configs/replay.yaml
```

### Utility Commands

#### `trader config`
Manages configuration files.

**Subcommands:**

**`trader config init`** - Generate default configuration
```bash
trader config init -o simulation.yaml
```

**`trader config validate`** - Validate configuration
```bash
trader config validate -f simulation.yaml
```

#### `trader journal`
Queries trade journal data from SQLite database.

**Subcommands:**

**`trader journal trade <id>`** - Get trade details
```bash
trader journal trade 01KFZ7PE7FKSDHSF0TJ06K5QXW
```

**`trader journal today`** - List today's closed trades
```bash
trader journal today
```

**`trader journal day <YYYY-MM-DD>`** - List trades for specific day
```bash
trader journal day 2024-01-15
```

**Flags:**
- `-d, --db` - Path to SQLite journal DB (default: ./trader.sqlite)

#### `trader oa2csv`
Downloads OANDA candle data to CSV format.

**Usage:**
```bash
trader oa2csv --token TOKEN -i INSTRUMENT --from TIME --to TIME [options]
```

**Flags:**
- `--token` - OANDA API token (or set OANDA_TOKEN env var)
- `-i, --instrument` - Instrument (default: EUR_USD)
- `--from` - RFC3339 start time (required)
- `--to` - RFC3339 end time (required)
- `--granularity` - Candlestick granularity (default: H1)
- `--price` - Price components: BA (bid/ask) or M (mid) (default: BA)
- `--out` - Output CSV path (default: oanda_ticks.csv)
- `--env` - Environment: practice or live (default: practice)

**Example:**
```bash
trader oa2csv --token YOUR_TOKEN -i EUR_USD \
  --from 2024-01-01T00:00:00Z \
  --to 2025-01-01T00:00:00Z \
  --granularity H1 \
  --out eurusd_2024.csv
```

### Demo Commands

#### `trader demo`
Runs example simulations for learning.

**Subcommands:**

**`trader demo basic`** - Simple single trade demo
```bash
trader demo basic
```

**`trader demo risk`** - Risk management demo
```bash
trader demo risk
```

**`trader demo simrun`** - Simple simulation runner
```bash
trader demo simrun
```

### Other Commands

#### `trader version`
Displays version information.

```bash
trader version
```

#### `trader help`
Shows help for any command.

```bash
trader help
trader help backtest
trader help config
```

## Migration from Old CLI Structure

### Before (Multiple Binaries)

The old structure had separate command directories:
- `cmd/backtest/` - Standalone backtest binary
- `cmd/oa2csv/` - Standalone oa2csv binary
- `cmd/replay/` - Standalone replay binary
- `cmd/trader/` - Multi-command CLI with run, config, journal

### After (Single Binary)

All commands are now consolidated into a single `trader` binary:
- `trader backtest` - Replaces `cmd/backtest/main.go`
- `trader oa2csv` - Replaces `cmd/oa2csv/main.go`
- `trader replay` - Enhanced version of old replay
- `trader run` - From old trader CLI
- `trader config` - From old trader CLI
- `trader journal` - From old trader CLI
- `trader demo` - New command consolidating examples

## Building

```bash
# Build the CLI
make build

# Binary will be at bin/trader
./bin/trader --help
```

## Testing

```bash
# Run all tests
make test

# Test specific commands
go test ./cmd/trader-cobra/cmd/...
```

## Benefits of Cobra-based Architecture

1. **Single Binary** - One binary to install and distribute
2. **Consistent UX** - All commands follow same patterns and conventions
3. **Better Help** - Auto-generated help text for all commands
4. **Tab Completion** - Shell completion support (bash, zsh, fish)
5. **Easier Discovery** - Users can explore all features with `trader help`
6. **Maintainability** - Shared code and utilities across commands
7. **Professional CLI** - Industry-standard CLI framework used by kubectl, docker, etc.
