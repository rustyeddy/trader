package trader

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func feedPlans(s *EMACross, closes []float64) []*StrategyPlan {
	out := make([]*StrategyPlan, 0, len(closes))
	for _, c := range closes {
		d := s.Update(mkClose(c))
		out = append(out, d)
	}
	return out
}

func TestEMACross_WarmupNoSignals(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
	})

	plans := feedPlans(s, []float64{1.0000, 1.0001, 1.0002, 1.0003})
	require.Len(t, plans, 4)
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Opens)
		require.Empty(t, plan.Closes)
	}
}

func TestEMACross_BaselineThenCrossUpThenCrossDown_NoTradePlans(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
		MinSpread:  0,
	})

	closes := make([]float64, 0, 200)

	// Phase 1: warm up with flat prices (no signals expected)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}

	// Phase 2: push relationship to a known non-zero baseline (fast < slow).
	// IMPORTANT: This will still not emit a SELL because baseline-first logic
	// uses the first non-zero rel as baseline.
	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}

	// Phase 3: strong uptrend to cross UP after baseline is established => BUY
	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	// Phase 4: strong downtrend to cross DOWN => SELL
	for i := 0; i < 30; i++ {
		p -= 0.0003
		closes = append(closes, p)
	}

	plans := feedPlans(s, closes)
	require.NotEmpty(t, plans)
	for _, plan := range plans {
		require.NotNil(t, plan)
		require.Empty(t, plan.Opens)
		require.Empty(t, plan.Closes)
	}
}

func TestEMACross_MinSpreadFiltersNoise(t *testing.T) {
	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
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
		Scale:      types.PriceScale,
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

	plans1 := feedPlans(s, closes)
	require.NotEmpty(t, plans1)

	s.Reset()

	plans2 := feedPlans(s, closes)
	require.Equal(t, plans1, plans2, "after reset, strategy should emit identical plans")
}

func TestEMACross_Name(t *testing.T) {
	s := NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: types.PriceScale})
	require.Equal(t, "EMA_CROSS(3,5)", s.Name())
}

func TestEMACrossPlan_Reason(t *testing.T) {
	s := NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: types.PriceScale})
	d := s.Update(mkClose(1.0))
	require.NotEmpty(t, d.Reason)
}

func TestNewEMACross_PanicOnInvalidConfig(t *testing.T) {
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 0, SlowPeriod: 5, Scale: types.PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 0, Scale: types.PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 5, SlowPeriod: 3, Scale: types.PriceScale})
	})
	require.Panics(t, func() {
		NewEMACross(EMACrossConfig{FastPeriod: 3, SlowPeriod: 5, Scale: 0})
	})
}
