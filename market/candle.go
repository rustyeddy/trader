package market

import "time"

// Candle represents OHLC (Open, High, Low, Close) candlestick data
type Candle struct {
	Open   float64
	High   float64
	Low    float64
	Close  float64
	time.Time
	Volume float64
}
