package trader

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func mkClose(close float64) market.Candle {
	toP := func(x float64) market.Price { return market.Price(x*float64(market.PriceScale) + 0.5) }
	return market.Candle{Close: toP(close)}
}

// testFake is a trivial in-package strategy registered under "fake" so trader
// package tests that exercise the registry (factory dispatch, summary
// generation) can compile and run without pulling in strategies/fake (which
// would be a circular import for the trader package's own tests).
type testFake struct{}

func (testFake) Name() string            { return "Fake" }
func (testFake) Reset()                  {}
func (testFake) Ready() bool             { return true }
func (testFake) StopDescription() string { return "" }
func (testFake) Update(_ context.Context, _ *market.CandleTime, _ strategy.StrategyContext) *strategy.StrategyPlan {
	return strategy.DefaultPlan()
}

func init() {
	build := func(map[string]any) (strategy.Strategy, error) {
		return testFake{}, nil
	}
	strategy.MustRegisterStrategy(build, "fake")
	strategy.MustRegisterStrategy(build, "noop", "no-op")
	strategy.MustRegisterStrategy(build, "fake-02")
	strategy.MustRegisterStrategy(build, "lifecycle-test")
	strategy.MustRegisterStrategy(build, "template")
	strategy.MustRegisterStrategy(build, "ema-cross")
	strategy.MustRegisterStrategy(build, "ema-cross-adx")
	strategy.MustRegisterStrategy(build, "donchian", "donchian-breakout")
}
