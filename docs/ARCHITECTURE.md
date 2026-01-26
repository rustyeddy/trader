# Trader Architecture Overview

## What This Codebase Does

A professional-grade FX trading simulator and research platform for
backtesting strategies with realistic accounting, risk management, and
margin enforcement.

## Core Components

### Trading Engine (`sim/`)
- Paper trading simulation with real-time price updates
- Market order execution with fill prices (ask for BUY, bid for SELL)
- Stop-loss and take-profit enforcement on every price tick
- Forced liquidation when margin requirements violated

### Replay System (`replay/`)
- Replays historical tick data from CSV files
- Supports scripted trading events (OPEN, CLOSE, OPEN_SLTP, CLOSE_ALL)
- Tick-by-tick simulation with optional event execution
- Configurable via standalone CSV or configuration files
- Integrates seamlessly with the trading engine and journal

### Risk Management (`risk/`)
- Position sizing based on risk percentage of equity
- Accounts for stop distance, pip values, and currency conversion
- Returns exact units to trade for desired risk exposure

### P/L Accounting
- Calculates P/L in quote currency first, converts to account currency
- Unrealized P/L: uses bid for long positions, ask for short positions
- Realized P/L: recorded when trades close via stop/TP/liquidation
- Core invariant: `Equity = Balance + UnrealizedPL`

### Margin System
- Tracks margin used per position (2% of notional by default)
- Maintains `FreeMargin = Equity - MarginUsed`
- Auto-liquidates worst-performing trades when `Equity < MarginUsed`

### Trade Journal (`journal/`)
- Records all trade closures with P/L, prices, timestamps
- Captures equity snapshots on every price update
- Output formats: CSV, SQLite, Org-mode
- Write-only: never affects engine state

### Supported Instruments (`market/`)
- EUR_USD: Euro/US Dollar
- USD_JPY: US Dollar/Japanese Yen
- Handles currency conversions between quote and account currencies

## Architecture Principles

**Thread-safe**: All engine operations protected by mutex
**Testable**: Broker interface allows simulation or live implementation
**Invariant enforcement**: Core accounting rules never violated
**Deterministic**: Same price sequence produces same results

## Example Usage

```go
// Initialize engine with starting capital
engine := sim.NewEngine(broker.Account{
    ID:       "SIM-001",
    Currency: "USD",
    Balance:  100_000,
    Equity:   100_000,
}, journal)

// Set market prices
engine.UpdatePrice(broker.Price{
    Instrument: "EUR_USD",
    Bid:        1.0849,
    Ask:        1.0851,
})

// Calculate risk-based position size
size := risk.Calculate(risk.Inputs{
    Equity:         100_000,
    RiskPct:        0.005,  // 0.5% risk
    EntryPrice:     1.0851,
    StopPrice:      1.0831,  // 20 pip stop
    PipLocation:    -4,
    QuoteToAccount: 1.0,
})

// Execute trade
engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
    Instrument: "EUR_USD",
    Units:      size.Units,
    StopLoss:   &stopPrice,
    TakeProfit: &targetPrice,
})
```

## Key Invariants

These always hold:
- Equity = Balance + UnrealizedPL
- FreeMargin = Equity − MarginUsed
- Every trade closed exactly once
- Forced liquidation never leaves Equity < MarginUsed
- BUY positions: open at ask, close at bid
- SELL positions: open at bid, close at ask

## Use Cases

1. Backtest trading strategies on historical FX data
2. Replay historical tick data with scripted trading events
3. Paper trade to validate strategies without capital risk
4. Research position sizing and risk management approaches
5. Analyze equity curves and drawdowns
6. Learn FX trading mechanics with proper accounting
