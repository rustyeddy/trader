package backtest

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticPlanStrategy struct {
	name string
	plan *trader.StrategyPlan
}

func (s *staticPlanStrategy) Name() string { return s.name }
func (s *staticPlanStrategy) Reset()       {}
func (s *staticPlanStrategy) Ready() bool  { return true }
func (s *staticPlanStrategy) Update(context.Context, *trader.CandleTime, *trader.Positions) *trader.StrategyPlan {
	return s.plan
}

func TestConfiguredStrategy_RiskSizerIgnoresStrategyUnits(t *testing.T) {
	t.Parallel()

	th := trader.NewTradeHistory("EURUSD")
	th.Side = trader.Long
	th.Stop = trader.PriceFromFloat(1.0990)

	open := &trader.OpenRequest{Request: trader.Request{TradeCommon: th.TradeCommon}}
	open.Units = 123456

	strat := &staticPlanStrategy{
		name: "test",
		plan: &trader.StrategyPlan{Opens: []*trader.OpenRequest{open}},
	}

	adapter := &configuredStrategy{S: strat, Sizer: riskSizer{}}
	ct := &trader.CandleTime{
		Candle: trader.Candle{Close: trader.PriceFromFloat(1.1000)},
	}

	plan := adapter.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Units(0), plan.Opens[0].Units)
}

func TestConfiguredStrategy_AppliesStopTakeThenRiskSizer(t *testing.T) {
	t.Parallel()

	th := trader.NewTradeHistory("EURUSD")
	th.Side = trader.Long

	open := &trader.OpenRequest{Request: trader.Request{TradeCommon: th.TradeCommon}}
	open.Units = 999

	strat := &staticPlanStrategy{
		name: "test",
		plan: &trader.StrategyPlan{Opens: []*trader.OpenRequest{open}},
	}

	adapter := &configuredStrategy{
		S:         strat,
		Sizer:     riskSizer{},
		StopPips:  20,
		TakePips:  40,
		PipScaled: trader.PipScaled(-4),
	}

	ct := &trader.CandleTime{
		Candle: trader.Candle{Close: trader.PriceFromFloat(1.1000)},
	}

	plan := adapter.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Units(0), plan.Opens[0].Units)
	assert.NotZero(t, plan.Opens[0].Stop)
	assert.NotZero(t, plan.Opens[0].Take)
	assert.Less(t, plan.Opens[0].Stop, ct.Candle.Close)
	assert.Greater(t, plan.Opens[0].Take, ct.Candle.Close)
}
