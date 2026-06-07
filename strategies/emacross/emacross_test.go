package emacross

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

func mkClose(close float64) trader.Candle {
	toP := func(x float64) trader.Price { return trader.Price(x*float64(trader.PriceScale) + 0.5) }
	return trader.Candle{Close: toP(close)}
}

func feedPlans(s *Cross, closes []float64) []*trader.StrategyPlan {
	out := make([]*trader.StrategyPlan, 0, len(closes))
	for _, c := range closes {
		d := s.Update(context.Background(), &trader.CandleTime{Candle: mkClose(c)}, nil)
		out = append(out, d)
	}
	return out
}

func TestCross_WarmupNoSignals(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      trader.PriceScale,
	})
	require.NoError(t, err)

	plans := feedPlans(s, []float64{1.0000, 1.0001, 1.0002, 1.0003})
	require.Len(t, plans, 4)
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Opens)
		require.Empty(t, plan.Closes)
	}
}

func TestCross_BaselineThenCrossUpThenCrossDown_EmitsOpenPlans(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      trader.PriceScale,
		MinSpread:  0,
	})
	require.NoError(t, err)

	closes := make([]float64, 0, 200)

	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}

	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}

	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	for i := 0; i < 30; i++ {
		p -= 0.0003
		closes = append(closes, p)
	}

	plans := feedPlans(s, closes)
	require.NotEmpty(t, plans)

	var opens []*trader.OpenRequest
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Closes, "no lots in test so no closes expected")
		opens = append(opens, plan.Opens...)
	}

	require.Len(t, opens, 2, "expect exactly one long open and one short open")
	require.Equal(t, trader.Long, opens[0].Side, "first open should be long (cross up)")
	require.Equal(t, trader.Short, opens[1].Side, "second open should be short (cross down)")
}

func TestCross_MinSpreadFiltersNoise(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      trader.PriceScale,
		MinSpread:  0.0010,
	})
	require.NoError(t, err)

	closes := make([]float64, 0, 64)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	closes = append(closes,
		1.0002, 1.0001, 1.0003, 1.0002, 1.0004, 1.0003,
		1.0002, 1.0001, 1.0000, 0.9999, 1.0000, 1.0001,
	)

	plans := feedPlans(s, closes)
	require.NotEmpty(t, plans)
	for _, plan := range plans {
		require.Empty(t, plan.Opens)
		require.Empty(t, plan.Closes)
	}
}

func TestCross_ResetReplaysSameSignalSequence(t *testing.T) {
	cfg := Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      trader.PriceScale,
		MinSpread:  0,
	}
	s, err := New(cfg)
	require.NoError(t, err)

	closes := make([]float64, 0, 64)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	closes = append(closes,
		1.0002, 1.0004, 1.0006, 1.0008, 1.0010, 1.0012, 1.0014, 1.0016,
		1.0014, 1.0012, 1.0010, 1.0008, 1.0006, 1.0004, 1.0002, 1.0000,
	)

	planSignature := func(plans []*trader.StrategyPlan) []string {
		var sigs []string
		for _, p := range plans {
			sigs = append(sigs, fmt.Sprintf("%s opens=%d closes=%d", p.Reason, len(p.Opens), len(p.Closes)))
		}
		return sigs
	}

	plans1 := feedPlans(s, closes)
	require.NotEmpty(t, plans1)

	s.Reset()

	plans2 := feedPlans(s, closes)
	require.Equal(t, planSignature(plans1), planSignature(plans2), "after reset, strategy should emit identical signal sequence")
}

func TestCross_Name(t *testing.T) {
	s, err := New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: trader.PriceScale})
	require.NoError(t, err)
	require.Equal(t, "EMA_CROSS(3,5)", s.Name())
}

func TestCrossPlan_Reason(t *testing.T) {
	s, err := New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: trader.PriceScale})
	require.NoError(t, err)
	d := s.Update(context.Background(), &trader.CandleTime{Candle: mkClose(1.0)}, nil)
	require.NotEmpty(t, d.Reason)
}

func TestNew_ErrorOnInvalidConfig(t *testing.T) {
	_, err := New(Config{FastPeriod: 0, SlowPeriod: 5, Scale: trader.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 3, SlowPeriod: 0, Scale: trader.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 5, SlowPeriod: 3, Scale: trader.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: 0})
	require.Error(t, err)
}
