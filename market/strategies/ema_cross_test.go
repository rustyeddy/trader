package strategies

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func feedSignals(s *EMACross, scale int32, closes []float64) []Decision {
	out := make([]Decision, 0, 8)
	for _, c := range closes {
		d := s.Update(mkClose(scale, c), scale)
		if d.Signal != Hold {
			out = append(out, d)
		}
	}
	return out
}

func TestEMACross_WarmupNoSignals(t *testing.T) {
	scale := int32(10000)

	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      scale,
	})

	events := feedSignals(s, scale, []float64{1.0000, 1.0001, 1.0002, 1.0003})
	require.Len(t, events, 0)
}

func TestEMACross_BaselineThenCrossUpThenCrossDown(t *testing.T) {
	scale := int32(10000)

	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      scale,
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

	events := feedSignals(s, scale, closes)

	// Expect at least BUY then SELL (baseline-first means no signal on baseline set)
	require.GreaterOrEqual(t, len(events), 2, "expected at least BUY then SELL")

	require.Equal(t, Buy, events[0].Signal, "first signal should be BUY (cross up after baseline)")

	foundSell := false
	for i := 1; i < len(events); i++ {
		if events[i].Signal == Sell {
			foundSell = true
			break
		}
	}
	require.True(t, foundSell, "expected a SELL after the BUY")
}

func TestEMACross_MinSpreadFiltersNoise(t *testing.T) {
	scale := int32(10000)

	s := NewEMACross(EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      scale,
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

	events := feedSignals(s, scale, closes)
	require.Len(t, events, 0)
}

func TestEMACross_ResetReplaysSameSignalSequence(t *testing.T) {
	scale := int32(10000)

	cfg := EMACrossConfig{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      scale,
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

	events1 := feedSignals(s, scale, closes)
	require.NotEmpty(t, events1)

	s.Reset()

	events2 := feedSignals(s, scale, closes)
	require.Equal(t, events1, events2, "after reset, strategy should emit identical signals")
}
