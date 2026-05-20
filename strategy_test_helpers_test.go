package trader

import "context"

func mkClose(close float64) Candle {
	toP := func(x float64) Price { return Price(x*float64(PriceScale) + 0.5) }
	return Candle{Close: toP(close)}
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
func (testFake) Update(ctx context.Context, c *CandleTime, run *Backtest) *StrategyPlan {
	_ = ctx
	_ = c
	_ = run
	return &DefaultStrategyPlan
}

func init() {
	build := func(map[string]any) (Strategy, error) {
		return testFake{}, nil
	}
	RegisterStrategy(build, "fake")
	RegisterStrategy(build, "noop", "no-op")
	RegisterStrategy(build, "fake-02")
	RegisterStrategy(build, "lifecycle-test")
	RegisterStrategy(build, "template")
	RegisterStrategy(build, "ema-cross")
	RegisterStrategy(build, "ema-cross-adx")
	RegisterStrategy(build, "donchian", "donchian-breakout")
}
