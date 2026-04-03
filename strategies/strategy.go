package strategies

import (
	"github.com/rustyeddy/trader/market"
)

// Strategy is the interface for candle-based strategies.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c market.Candle) *Plan
}

type Float64 interface {
	Float64() float64
}

type Price interface {
	Price() Price
}

type StrategyConfig struct {
	Instrument string
}
