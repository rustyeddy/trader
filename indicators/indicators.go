// Package indicators provides technical analysis indicators for trading
package indicators

import "github.com/rustyeddy/trader/market"

// Indicator computes a single streaming value from candles.
// It is deterministic and safe to use in live, replay, and backtests.
type Indicator interface {
	// Name returns a stable identifier like "EMA(20)" or "RSI(14)".
	Name() string

	// Warmup returns how many updates are needed before Ready() can be true.
	// (Some indicators may become ready earlier; that's fine.)
	Warmup() int

	// Reset clears all internal state.
	Reset()

	// Update consumes the next *closed* candle and updates internal state.
	Update(c market.Candle)

	// Ready reports whether Value() is meaningful (warmup completed).
	Ready() bool
}

type ValueF64 interface {
	// Value returns the current indicator value. If !Ready(), it should return 0
	// (or the last computed value) — callers should always check Ready().
	Value() float64
}

type ValueI32 interface {
	Value() int32
}
