---
name: project-data-audit
description: Planned feature to audit candle data for missing or empty tick files during market-open hours
metadata:
  type: project
---

Audit candles for gaps and missing bi5 tick files.

**Why:** After bulk-downloading Dukascopy ticks, some hourly bi5 files may be missing or empty (network errors, weekend/holiday edge cases, partial hours). Generated candles for those hours will be zero-filled placeholders rather than real price data.

**How to apply:** When this feature comes up, the approach is:
- Walk generated H1 CSV candles for each instrument over the full date range
- For every bar during expected forex market-open hours (Sun 17:00 – Fri 17:00 US Eastern, approximately), flag any candle where all OHLC values are zero
- Cross-reference against the bi5 file inventory in `data/dukascopy/` to identify which specific hourly files are missing or suspiciously small
- Re-download only those files, then rebuild just the affected monthly candle CSVs
