---
name: project-data-source-abstraction
description: Planned architecture for pluggable tick data sources (Dukascopy, TrueFX, OANDA, etc.)
metadata:
  type: project
---

Formalize a `TickSource` interface so any data provider can feed the candle pipeline without changing downstream code.

**Why:** Dukascopy has sparse data for AUD/NZD/CAD. Plan is to add TrueFX for those pairs (better institutional-quality tick coverage from ~2009). Longer term, any source should be swappable.

**Architecture:**
```
TickSource interface  (per instrument, selectable)
    ↓
iterator[RawTick]     ← canonical format; already exists
    ↓
buildHourM1FromTickIterator  ← already source-agnostic
    ↓
candleSet / CSV
```

**How to apply:**
- Define `TickSource` interface with `OpenTickIterator(ctx, key) → iterator[RawTick]`
- Wrap existing bi5 reader as `DukascopySource`
- Add `TrueFXSource` reading their monthly CSV ZIPs
- `DataManager` selects source per instrument (EURUSD/GBPUSD/USDJPY/USDCHF → Dukascopy; AUDUSD/NZDUSD/USDCAD → TrueFX)
- `RawTick` and `iterator[RawTick]` are already generic — candle builder is already source-agnostic

**Current state (2026-05-18):**
- Dukascopy: complete for EUR/GBP/JPY/CHF (2003/2004–present); sparse for AUD/NZD/CAD
- TrueFX integration: not yet started — lower priority than other backtest features
- Candles: only built for EUR/GBP/JPY/CHF; AUD/NZD/CAD pending TrueFX

**Related:** [[project-data-audit]]
