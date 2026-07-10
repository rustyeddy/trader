#!/usr/bin/env bash
# Examples: trader review --asof / --from/--to/--interval historical sweep
# (docs/asof-review-sweep-spec.md §4)

# Single historical date — table/json/org all work, same as a live run
./bin/trader review --instruments EURUSD --asof 2026-05-15 --output table

# Date range sweep — needs csv or json (table/org have no room for a Date column)
./bin/trader review --instruments EURUSD,GBPUSD --from 2026-05-01 --to 2026-05-10 --output csv

# Optional --interval controls the step size (default 24h), e.g. every 3 days:
./bin/trader review --instruments EURUSD --from 2026-05-01 --to 2026-06-01 --interval 72h --output csv
