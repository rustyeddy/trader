package strategies

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
)

type StrategyRegistry map[string]Strategy

var (
	registry = make(map[string]Strategy)
)

// Strategy is the interface for candle-based strategies.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c market.Candle) Decision
}

// TickStrategy is the minimal interface a tick-based backtest strategy must implement.
// It is called once per tick.
type TickStrategy interface {
	Name() string
	OnTick(ctx context.Context, b broker.Broker, tick market.Tick) error
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

func Register(name string, strat Strategy) {
	registry[name] = strat
}

func GetStrategy(name string) (strat Strategy) {
	var ok bool
	if strat, ok = registry[name]; !ok {
		return nil
	}
	return strat
}
