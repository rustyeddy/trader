export OANDA_TOKEN="..."
export OANDA_ACCOUNT_ID="..."

# stream for 30 seconds
go run . data oanda ticks --instruments EUR_USD --out eurusd_ticks.csv --duration 30s

# or stop after N ticks
go run . data oanda ticks --instruments EUR_USD,USD_JPY --out ticks.csv --max 500

go run . replay pricing --ticks eurusd_ticks.csv
go run . backtest ema-cross --ticks eurusd_ticks.csv --instrument EUR_USD
