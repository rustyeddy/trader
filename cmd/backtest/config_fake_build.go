package backtest

import (
	"context"

	"github.com/rustyeddy/trader"
)

type configFakeStrategy struct {
	instrument string
	opened     bool
}

func newConfigFakeStrategy(instrument string) *configFakeStrategy {
	return &configFakeStrategy{instrument: instrument}
}

func (s *configFakeStrategy) Name() string { return "Fake" }

func (s *configFakeStrategy) Reset() {
	s.opened = false
}

func (s *configFakeStrategy) Ready() bool { return true }

func (s *configFakeStrategy) Update(ctx context.Context, candle *trader.CandleTime, positions *trader.Positions) *trader.StrategyPlan {
	_ = ctx
	_ = positions
	if s.opened {
		return &trader.DefaultStrategyPlan
	}
	if candle == nil {
		return &trader.DefaultStrategyPlan
	}

	s.opened = true
	th := trader.NewTradeHistory(s.instrument)
	th.Side = trader.Long

	return &trader.StrategyPlan{
		Reason: "fake-open",
		Opens: []*trader.OpenRequest{
			{
				Request: trader.Request{
					TradeCommon: th.TradeCommon,
					RequestType: trader.RequestMarketOpen,
					Candle:      candle.Candle,
					Reason:      "fake-open",
				},
			},
		},
	}
}
