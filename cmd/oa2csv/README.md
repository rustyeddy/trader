```bash
export OANDA_TOKEN="YOUR_TOKEN"

go run ./cmd/oanda2csv \
  -env practice \
  -instrument EUR_USD \
  -granularity H1 \
  -price BA \
  -from 2024-01-01T00:00:00Z \
  -to   2025-01-01T00:00:00Z \
  -out  data/eurusd-h1-ticks.csv
```
