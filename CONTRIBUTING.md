# Contributing to Trader

Thank you for your interest in contributing to the Trader FX simulation platform! This guide will help you get started with development.

## Development Setup

### Prerequisites

- Go 1.25 or later
- Git
- Make (optional, but recommended)
- SQLite3 (for journal features)

### Setting Up Your Environment

1. Fork the repository on GitHub

2. Clone your fork:
```bash
git clone https://github.com/YOUR_USERNAME/trader.git
cd trader
```

3. Add the upstream remote:
```bash
git remote add upstream https://github.com/rustyeddy/trader.git
```

4. Install dependencies:
```bash
go mod download
```

5. Verify your setup:
```bash
make test
```

## Project Structure

```
trader/
├── broker/       # Broker interface and account models
├── cmd/          # Command-line applications
│   ├── simrun/   # Simple simulation runner
│   └── trader/   # Journal query CLI
├── docs/         # Architecture and design docs
├── id/           # ULID generation for trade IDs
├── journal/      # Trade and equity journaling (CSV, SQLite)
├── market/       # FX instrument metadata and conversions
├── risk/         # Position sizing calculations
├── sim/          # Paper trading engine
└── strategy/     # Trading strategy implementations
```

## Coding Standards

### General Guidelines

- Follow standard Go conventions and idioms
- Run `gofmt` before committing (or use an editor that formats on save)
- Keep functions focused and small (< 50 lines where possible)
- Add comments for exported functions and complex logic
- Write tests for new functionality

### Code Style

```go
// Good: Clear, descriptive names
func CalculatePositionSize(equity, riskPct float64) float64

// Bad: Abbreviated, unclear names
func CalcPosSize(e, r float64) float64

// Good: Early returns reduce nesting
func Validate(order Order) error {
    if order.Units == 0 {
        return errors.New("units cannot be zero")
    }
    if order.Instrument == "" {
        return errors.New("instrument required")
    }
    return nil
}

// Bad: Deeply nested conditions
func Validate(order Order) error {
    if order.Units != 0 {
        if order.Instrument != "" {
            return nil
        } else {
            return errors.New("instrument required")
        }
    }
    return errors.New("units cannot be zero")
}
```

### Testing

- Write table-driven tests where appropriate
- Use `testify/assert` for clearer assertions
- Test edge cases and error conditions
- Aim for meaningful test coverage (not just high percentages)

Example test structure:

```go
func TestCalculatePositionSize(t *testing.T) {
    tests := []struct {
        name     string
        inputs   risk.Inputs
        expected float64
    }{
        {
            name: "EUR_USD with 20 pip stop",
            inputs: risk.Inputs{
                Equity:      100_000,
                RiskPct:     0.01,
                EntryPrice:  1.0850,
                StopPrice:   1.0830,
                PipLocation: -4,
                QuoteToAccount: 1.0,
            },
            expected: 5000,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := risk.Calculate(tt.inputs)
            assert.Equal(t, tt.expected, result.Units)
        })
    }
}
```

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make cover

# Run tests for a specific package
go test ./sim

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v ./sim -run TestEngineMarginCall
```

## Core Invariants (Non-Negotiable)

When contributing code, these invariants **must always hold**:

### Accounting
- `Equity = Balance + UnrealizedPL`
- `FreeMargin = Equity - MarginUsed`
- Equity never jumps except due to price movement or trade open/close

### P/L Calculation
- P/L calculated in quote currency, then converted to account currency
- Conversion rate applied **once**
- BUY positions use bid price to close
- SELL positions use ask price to close

### Stop Loss / Take Profit
- SL/TP evaluated on every price update
- Stop price is inclusive (triggers when reached)
- Close price = triggering bid/ask (not the stop price)

### Margin Management
- Margin calculated using mid price
- Margin recomputed after every trade close
- Forced liquidation never leaves `Equity < MarginUsed`

### Journaling
- Every trade closed exactly once
- Equity snapshots monotonic in time
- Journal writes never affect engine state

## Making Changes

### Workflow

1. Create a feature branch:
```bash
git checkout -b feature/your-feature-name
```

2. Make your changes, following the coding standards

3. Add tests for new functionality

4. Run tests and ensure they pass:
```bash
make test
```

5. Commit your changes:
```bash
git add .
git commit -m "Add feature: brief description"
```

6. Push to your fork:
```bash
git push origin feature/your-feature-name
```

7. Open a Pull Request on GitHub

### Commit Messages

Write clear, descriptive commit messages:

```
Good:
- "Add support for GBP/USD instrument"
- "Fix margin calculation for negative positions"
- "Improve error handling in journal.RecordTrade"

Bad:
- "fix bug"
- "updates"
- "WIP"
```

## Types of Contributions

### Bug Fixes

1. Check if the issue is already reported
2. If not, open an issue describing the bug
3. Reference the issue number in your PR

### New Features

1. Open an issue to discuss the feature first
2. Wait for maintainer feedback before investing significant time
3. Implement the feature following the guidelines above
4. Update documentation as needed

### Documentation

Documentation improvements are always welcome:
- Fix typos or unclear wording
- Add examples or clarifications
- Improve code comments
- Update architecture docs for significant changes

### New Instruments

To add support for a new currency pair:

1. Add instrument metadata to `market/instruments.go`
2. Add conversion logic to `market/conversion.go` if needed
3. Add test cases in `market/conversion_test.go`
4. Update README.md to list the new instrument

Example:
```go
// In market/instruments.go
var Instruments = map[string]Meta{
    "GBP_USD": {
        Quote:       "USD",
        PipLocation: -4,
        MinSize:     1,
    },
    // ... existing instruments
}
```

### New Strategies

To contribute a trading strategy:

1. Create a new file in `strategy/` (e.g., `strategy/moving_average.go`)
2. Implement your strategy logic
3. Add tests in `strategy/moving_average_test.go`
4. Document the strategy's approach and parameters
5. Optionally add an example in `examples/`

## Review Process

1. Maintainers will review your PR for:
   - Code quality and style
   - Test coverage
   - Documentation
   - Adherence to core invariants

2. Address any feedback by pushing new commits to your branch

3. Once approved, maintainers will merge your PR

## Questions?

- Open an issue for general questions
- Tag issues with `question` label
- Be patient and respectful

## License

By contributing, you agree that your contributions will be licensed under the same license as the project (see LICENSE file).
