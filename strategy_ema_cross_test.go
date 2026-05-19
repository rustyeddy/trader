package trader

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func feedPlans(s *EMACross, closes []float64) []*StrategyPlan {
	out := make([]*StrategyPlan, 0, len(closes))
	for _, c := range closes {
		d := s.Update(context.Background(), &CandleTime{Candle: mkClose(c)}, nil)
		out = append(out, d)
	}
	return out
}

func TestEMACross_WarmupNoSignals(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      PriceScale,
	})

	plans := feedPlans(s, []float64{1.0000, 1.0001, 1.0002, 1.0003})
	require.Len(t, plans, 4)
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Opens)
		require.Empty(t, plan.Closes)
	}
}

func TestEMACross_BaselineThenCrossUpThenCrossDown_EmitsOpenPlans(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      PriceScale,
		MinSpread:  0,
	})

	closes := make([]float64, 0, 200)

	// Phase 1: warm up with flat prices (no signals expected)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}

	// Phase 2: push fast < slow to establish baseline (no open on baseline)
	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}

	// Phase 3: strong uptrend → cross UP after baseline → expect one long open
	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	// Phase 4: strong downtrend → cross DOWN → expect one short open
	for i := 0; i < 30; i++ {
		p -= 0.0003
		closes = append(closes, p)
	}

	plans := feedPlans(s, closes)
	require.NotEmpty(t, plans)

	var opens []*OpenRequest
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Closes, "no lots in test so no closes expected")
		opens = append(opens, plan.Opens...)
	}

	require.Len(t, opens, 2, "expect exactly one long open and one short open")
	require.Equal(t, Long, opens[0].Side, "first open should be long (cross up)")
	require.Equal(t, Short, opens[1].Side, "second open should be short (cross down)")
}

func TestEMACross_MinSpreadFiltersNoise(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      PriceScale,
		MinSpread:  0.0010, // big filter
	})

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

func TestEMACross_ResetReplaysSameSignalSequence(t *testing.T) {
	cfg := EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      PriceScale,
		MinSpread:  0,
	}
	s := NewEMACross(cfg)

	closes := make([]float64, 0, 64)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	closes = append(closes,
		1.0002, 1.0004, 1.0006, 1.0008, 1.0010, 1.0012, 1.0014, 1.0016,
		1.0014, 1.0012, 1.0010, 1.0008, 1.0006, 1.0004, 1.0002, 1.0000,
	)

	planSignature := func(plans []*StrategyPlan) []string {
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

func TestEMACross_Name(t *testing.T) {
	s := NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: PriceScale})
	require.Equal(t, "EMA_CROSS(3,5)", s.Name())
}

func TestEMACrossPlan_Reason(t *testing.T) {
	s := NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: PriceScale})
	d := s.Update(context.Background(), &CandleTime{Candle: mkClose(1.0)}, nil)
	require.NotEmpty(t, d.Reason)
}

func TestNewEMACross_PanicOnInvalidConfig(t *testing.T) {
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 0, SlowPeriod: 5, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 0, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 5, SlowPeriod: 3, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: 0})
	})
}
