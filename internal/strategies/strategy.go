package strategies

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/broker"
)

type StrategyRegistry map[string]TickStrategy

var (
	registry = make(map[string]TickStrategy)
)

// TickStrategy is the minimal interface a backtest strategy must implement.
// It is called once per CSV row (tick).
type TickStrategy interface {
	OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error
}

// TradeClosedListener is an optional interface that strategies can implement
// to be notified when the engine auto-closes one of their trades (e.g., due to stop loss or take profit).
// This allows strategies to update their internal state accordingly.
type TradeClosedListener interface {
	OnTradeClosed(tradeID string, reason string)
}

func Register(name string, strat TickStrategy) {
	registry[name] = strat
}

func GetStrategy(name string) (strat TickStrategy) {
	var ok bool
	if strat, ok = registry[name]; !ok {
		return nil
	}
	return strat
}

// strategByName needs to be redone since it mixes some strategy elements with
// some risk management elements
func StrategyByName(name string, instrument string, units float64, fast, slow int, riskPct, stopPips, rr float64) (TickStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "noop", "none":
		return NoopStrategy{}, nil

	case "open-once":
		return &OpenOnceStrategy{
			Instrument: instrument,
			Units:      units,
		}, nil

	case "ema-cross", "emacross":
		return NewEmaCross(instrument, fast, slow, riskPct, stopPips, rr), nil

	default:
		return nil, fmt.Errorf("unknown strategy %q (supported: noop, open-once)", name)
	}
}
