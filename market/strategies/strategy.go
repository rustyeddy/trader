package strategies

import (
	"github.com/rustyeddy/trader/market"
)

type StrategyRegistry map[string]Strategy

var (
	registry = make(map[string]Strategy)
)

// TickStrategy is the minimal interface a backtest strategy must implement.
// It is called once per CSV row (tick).
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c market.Candle) Decision
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
