package strategies

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
)

// Strategy is the interface for candle-based strategies.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c market.Candle) Decision

	New(cfg any) Strategy
}

// TickStrategy is the minimal interface a tick-based backtest strategy must implement.
// It is called once per tick.
type TickStrategy interface {
	OnTick(ctx context.Context, b broker.Broker, tick market.Tick) error
}

type Float64 interface {
	Float64() float64
}

type Price interface {
	Price() Price
}

type StrategyConfig struct {
	Balance float64
	Stop    int32 // pips
	Take    int32 // pips
	RR      float64

	File string // string to the file
}

type Signal int

const (
	Hold Signal = iota
	Buy
	Sell
)

func (s Signal) String() string {
	switch s {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

type Decision interface {
	Signal() Signal
	Reason() string
}
