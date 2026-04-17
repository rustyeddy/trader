package strategies

import (
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/types"
)

var l = log.Strat

// Strategy is the interface for candle-based strategies.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c types.Candle) *Plan
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
