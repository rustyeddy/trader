package backtest

import "github.com/rustyeddy/trader"

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

func (s *configFakeStrategy) Update(c trader.Candle) *trader.StrategyPlan {
	if s.opened {
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
					Candle:      c,
					Reason:      "fake-open",
				},
			},
		},
	}
}
