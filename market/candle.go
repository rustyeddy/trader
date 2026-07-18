package market

import (
	"fmt"

	"github.com/rustyeddy/trader/types"
)

// Candle represents a trader domain type. Timestamp is the candle's true
// observed open time — authoritative and stored verbatim, never
// reconstructed from array position (see #179: reconstructing it from
// Start+idx*step is what caused D1/H4 candles to silently mislabel across
// a DST transition). A zero Timestamp on a synthetic/test candle means
// "this candle is genuinely timeless," by convention, not enforced by the
// type system.
type Candle struct {
	Open      types.Price
	High      types.Price
	Low       types.Price
	Close     types.Price
	AvgSpread types.Price
	MaxSpread types.Price
	Ticks     int32 // number of ticks per candle
	Timestamp types.Timestamp
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

// String is an internal helper for trader type processing. Value receiver
// (not pointer) so market.Candle values — not just pointers — satisfy
// fmt.Stringer, matching the old market.CandleTime's value-receiver String().
func (c Candle) String() string {
	return fmt.Sprintf("%s, %s, %s, %s", c.Open, c.High, c.Low, c.Close)
}

// FullString is an internal helper for trader type processing.
func (c Candle) FullString() string {
	return fmt.Sprintf("%s, %s, %s, %s: avg spread %s, max spread %s, ticks: %d",
		c.Open, c.High, c.Low, c.Close, c.AvgSpread, c.MaxSpread, c.Ticks)
}

// CandleIterator traverses a sequence of timestamped candles. It is the
// data-access contract: the datamanager layer produces iterators, the engine
// consumes them, so both depend only on this interface, not each other.
type CandleIterator interface {
	Next() (Candle, bool)
	Err() error
	Close() error
}
