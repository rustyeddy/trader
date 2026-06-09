# Candle Service Layer Plan

## Goal

Provide one shared service-layer path that reads local candle data and returns it in the repository's canonical candle CSV format. Expose that shared service through:

- CLI: `trader data candles --instrument EURUSD --from YYYY-MM-DD [--to YYYY-MM-DD] --timeframe H1`
- REST: `GET /api/v1/candles/{instrument}?from=YYYY-MM-DD&to=YYYY-MM-DD&timeframe=H1`
- MCP: `get_candles_csv` returning candle CSV data to an AI agent

## Canonical CSV format

The service should emit the same scaled integer format used by the candle store:

```csv
# schema=v1 source=oanda instrument=EURUSD tf=h1 scale=100000
Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags
1704067200,110100,110000,109900,110050,10,15,60,0x0001
```

Prices and spreads remain scaled integers. Float conversion belongs at input/output display boundaries only, not in internal candle traversal or formatting.

## Behavior

- Required inputs: instrument, from date, timeframe.
- Optional `to`: defaults to current UTC time when omitted.
- User-facing `to` dates are inclusive; internally the service converts them to the existing exclusive `TimeRange.End`.
- Instrument names are normalized through existing helpers.
- Timeframe parsing reuses existing `Timeframe` parsing.
- Data source defaults to the local OANDA canonical store, with an optional source override.
- Service returns CSV data plus metadata such as instrument, timeframe, from, to, source, and candle count.

## Implementation

1. Add `service.CandlesCSVRequest`, `service.CandlesCSVResult`, and `Service.CandlesCSV(ctx, req)`.
2. Reuse `DataManager.Candles` to stream `CandleTime` values, avoiding duplicated store traversal in CLI/REST/MCP.
3. Add a reusable CSV writer for `CandleTime` rows in canonical format.
4. Add CLI command `trader data candles` that writes CSV to stdout.
5. Add REST route `GET /api/v1/candles/{instrument}` that returns `text/csv`.
6. Add MCP read tool `get_candles_csv` with instrument/timeframe/from/to/source args.
7. Add service and edge-layer tests for defaults, CSV shape, and routing.

## Redundancy cleanup

- Reuse `Service.CandlesCSV` or a shared lower-level candle streaming helper anywhere an edge layer currently opens `DataManager.Candles` directly.
- Keep `GET /api/v1/backtests/{name}/candles` only for chart-friendly JSON tied to a saved backtest report, but route candle loading through the service layer where practical.
- Consider moving replay bar-loading toward the same candle service helper so REST replay and future CLI/MCP replay do not duplicate candle traversal or OHLC conversion.
- Keep `trader data stats` as aggregate analysis output, but make future raw candle output use `trader data candles` rather than adding ad hoc commands.
- MCP should expose one canonical read tool for candle CSV; `download_candles` remains write-only and should not duplicate read/export behavior.

## Notes

- Keep CLI/REST/MCP thin: parse inputs, call service, return output.
- Do not put business/store traversal logic in command handlers or HTTP/MCP handlers.
- The service reads the local canonical candle store; `trader data oanda` and MCP `download_candles` remain responsible for fetching and writing candles.
