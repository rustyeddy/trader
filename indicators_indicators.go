// Package indicators provides technical analysis indicators for trading
package trader

// CandleIndicator computes a single streaming value from candles.
// It is deterministic and safe to use in live, replay, and backtests.
type CandleIndicator interface {
	// Name returns a stable identifier like "EMA(20)" or "RSI(14)".
	Name() string

	// Warmup returns how many updates are needed before Ready() can be true.
	// (Some indicators may become ready earlier; that's fine.)
	Warmup() int

	// Reset clears all internal state.
	Reset()

	// Update consumes the next *closed* candle and updates internal state.
	Update(c Candle)

	// Ready reports whether Value() is meaningful (warmup completed).
	Ready() bool
}

type IndicatorFloat64 interface {
	// Value returns the current indicator value. If !Ready(), it should return 0
	// (or the last computed value) — callers should always check Ready().
	Float64() float64
}

type IndicatorFloat64s interface {
	// Value returns the current indicator value. If !Ready(), it should return 0
	// (or the last computed value) — callers should always check Ready().
	Float64() []float64
}

type IndicatorPrice interface {
	Price() Price
}
