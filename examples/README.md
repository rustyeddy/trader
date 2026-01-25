# Trading Strategy Examples

This directory contains example trading strategies demonstrating how to use the Trader platform.

## Examples

### Basic Examples

- **[basic_trade.go](basic_trade.go)** - Simple single trade with stop loss and take profit
- **[multiple_trades.go](multiple_trades.go)** - Opening and managing multiple positions
- **[risk_management.go](risk_management.go)** - Demonstrates proper risk-based position sizing

### Running Examples

Each example is a standalone Go program that can be run with:

```bash
go run examples/basic_trade.go
```

## Creating Your Own Strategy

1. Start with one of the basic examples as a template
2. Modify the trading logic to implement your strategy
3. Add proper risk management using the `risk` package
4. Test with simulated data before live trading
5. Review output in `trades.csv` and `equity.csv`

## Key Concepts

### Setting Up the Engine

```go
// Create journal for recording trades
j, err := journal.NewCSV("./trades.csv", "./equity.csv")
if err != nil {
    panic(err)
}

// Initialize engine with starting capital
engine := sim.NewEngine(broker.Account{
    ID:       "SIM-001",
    Currency: "USD",
    Balance:  100_000,
    Equity:   100_000,
}, j)
```

### Setting Prices

```go
// Set initial price
engine.Prices().Set(broker.Price{
    Instrument: "EUR_USD",
    Bid:        1.0849,
    Ask:        1.0851,
})

// Update price (triggers stop checks)
engine.UpdatePrice(broker.Price{
    Instrument: "EUR_USD",
    Bid:        1.0850,
    Ask:        1.0852,
    Time:       time.Now(),
})
```

### Calculating Position Size

```go
meta := market.Instruments["EUR_USD"]
price, _ := engine.GetPrice(ctx, "EUR_USD")

size := risk.Calculate(risk.Inputs{
    Equity:         acct.Equity,
    RiskPct:        0.01,           // Risk 1% per trade
    EntryPrice:     price.Ask,
    StopPrice:      price.Ask - 0.0020,  // 20 pips
    PipLocation:    meta.PipLocation,
    QuoteToAccount: 1.0,            // USD quote currency
})
```

### Opening a Trade

```go
stopPrice := price.Ask - 0.0020   // 20 pips below entry
targetPrice := price.Ask + 0.0040 // 40 pips above entry (2:1 R:R)

_, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
    Instrument: "EUR_USD",
    Units:      size.Units,        // From risk calculation
    StopLoss:   &stopPrice,
    TakeProfit: &targetPrice,
})
```

## Tips

1. **Always use stop losses** - Protect your capital
2. **Risk 0.5-2% per trade** - Never risk more than you can afford to lose
3. **Test extensively** - Use the simulator before real money
4. **Keep a journal** - Review trades to improve
5. **Respect the invariants** - The platform enforces professional accounting rules

## Contributing

Have a great strategy example? Please contribute it! See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines.
