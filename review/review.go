package review

import "time"

// ReviewResult is the per-pair output of one watchlist review run.
// All price-scaled values are converted to float64 for presentation.
// ATR-normalized values are dimensionless ratios.
type ReviewResult struct {
	Instrument string    `json:"instrument"`
	ScannedAt  time.Time `json:"scanned_at"`

	// Triage bucket.
	Bucket string `json:"bucket"` // "watch" | "hot" | "tradeable"

	// Directional bias from indicator combination.
	Bias string `json:"bias"` // "long" | "short" | "neutral"

	// Multi-timeframe snapshots.
	W1    W1Snapshot    `json:"w1"`
	D1    D1Snapshot    `json:"d1"`
	H4    H4Snapshot    `json:"h4"`
	Setup SetupSnapshot `json:"setup"`

	// Human-readable notes for tooltip (e.g. "ADX rising, H4 squeeze").
	Notes []string `json:"notes,omitempty"`
}

type D1Snapshot struct {
	ADX     float64 `json:"adx"`
	PlusDI  float64 `json:"plus_di"`
	MinusDI float64 `json:"minus_di"`
	CI      float64 `json:"ci"`
	ATRPips float64 `json:"atr_pips"`
	EMA20   float64 `json:"ema20"`
	EMA50   float64 `json:"ema50"`
	Close   float64 `json:"close"`

	// Derived.
	EMASepATR     float64 `json:"ema_sep_atr"`     // (EMA20-EMA50)/ATR14
	PriceEMA20ATR float64 `json:"price_ema20_atr"` // (Close-EMA20)/ATR14
	BBPctB        float64 `json:"bb_pct_b"`
	BBWidthATR    float64 `json:"bb_width_atr"`
	TrendPct      float64 `json:"trend_pct"` // % of last 20 bars trending
}

type H4Snapshot struct {
	ADX           float64 `json:"adx"`
	CI            float64 `json:"ci"`
	ATRPips       float64 `json:"atr_pips"`
	EMA20         float64 `json:"ema20"`
	Close         float64 `json:"close"`
	PriceEMA20ATR float64 `json:"price_ema20_atr"` // (Close-EMA20)/H4 ATR14
	Squeeze       bool    `json:"squeeze"`         // BBWidthATR below threshold
}

type W1Snapshot struct {
	EMA20       float64 `json:"ema20"`
	Close       float64 `json:"close"`
	ATRPips     float64 `json:"atr_pips"`
	WeekUsedPct float64 `json:"week_used_pct"` // current range / avg weekly ATR
}

type SetupSnapshot struct {
	// Nearest-timeframe EMA distance; H4 is preferred when ready.
	PriceEMAATR float64 `json:"price_ema_atr"`
	InValueZone bool    `json:"in_value_zone"` // |PriceEMAATR| in [0.5, 1.5]
	Squeeze     bool    `json:"squeeze"`       // H4 BB squeeze
	H4Aligned   bool    `json:"h4_aligned"`    // H4 bias matches D1 bias
	W1Aligned   bool    `json:"w1_aligned"`    // W1 bias matches D1 bias
}
