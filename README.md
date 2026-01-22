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
go run ./cmd/simrun
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

