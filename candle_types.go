package trader

import "fmt"

// Candle represents a trader domain type.
type Candle struct {
	Open      Price
	High      Price
	Low       Price
	Close     Price
	AvgSpread Price
	MaxSpread Price
	Ticks     int32 // number of ticks per candle
}

// IsZero is an internal helper for trader type processing.
func (c *Candle) IsZero() bool {
	return c.Open == 0 && c.High == 0 && c.Low == 0 && c.Close == 0 && c.Ticks == 0
}

// Validate reports whether the candle has a valid OHLC shape.
// High == Low is permitted (flat/doji candle — common in M1 thin-market minutes).
func (c Candle) Validate() bool {
	return c.High >= c.Low &&
		c.Open >= c.Low && c.Open <= c.High &&
		c.Close >= c.Low && c.Close <= c.High
}

// String is an internal helper for trader type processing.
func (c *Candle) String() string {
	return fmt.Sprintf("%s, %s, %s, %s", c.Open, c.High, c.Low, c.Close)
}

// FullString is an internal helper for trader type processing.
func (c *Candle) FullString() string {
	return fmt.Sprintf("%s, %s, %s, %s: avg spread %s, max spread %s, ticks: %d",
		c.Open, c.High, c.Low, c.Close, c.AvgSpread, c.MaxSpread, c.Ticks)
}

// candleTime represents a trader domain type.
type candleTime struct {
	Candle
	Timestamp
}

// CandleTime represents a trader domain type.
type CandleTime = candleTime

// String is an internal helper for trader type processing.
func (c candleTime) String() string {
	return c.Candle.String()
}
