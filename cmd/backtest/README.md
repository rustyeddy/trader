```go
go run ./cmd/backtest \
  -ticks data/eurusd-h1-ticks.csv \
  -db ./backtest.sqlite \
  -strategy open-once \
  -instrument EUR_USD \
  -units 10000 \
  -close-end=true
```

