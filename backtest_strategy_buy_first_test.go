package trader

import "context"

// BuyFirstBarStrategy is a test-only strategy that buys on the first bar and holds.
type BuyFirstBarStrategy struct {
	done bool
}

func (s *BuyFirstBarStrategy) Name() string {
	return "buy-first-bar"
}

func (s *BuyFirstBarStrategy) Reset() {
	s.done = false
}

func (s *BuyFirstBarStrategy) Ready() bool {
	return true
}

func (s *BuyFirstBarStrategy) Update(ctx context.Context, candle *CandleTime, positions *Positions) *StrategyPlan {
	if candle == nil || s.done || (positions != nil && positions.Len() > 0) {
		return &DefaultStrategyPlan
	}

	s.done = true

	th := NewTradeHistory(StrategyInstrument(ctx))
	th.Side = Long
	th.Units = Units(1000)
	return &StrategyPlan{
		Reason: "enter on first bar",
		Opens: []*OpenRequest{{
			Request: Request{TradeCommon: th.TradeCommon, Price: candle.Close, Timestamp: candle.Timestamp, Reason: "enter on first bar"},
		}},
	}
}
