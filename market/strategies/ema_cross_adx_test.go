package strategies

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

func mkOHLC(scale int32, o, h, l, c float64) market.OHLC {
	toP := func(x float64) market.Price { return market.Price(x*float64(scale) + 0.5) }
	return market.OHLC{
		O: toP(o),
		H: toP(h),
		L: toP(l),
		C: toP(c),
	}
}

func feedSignalsADX(s *EMACrossADX, scale int32, candles []market.OHLC) []Decision {
	out := make([]Decision, 0, 16)
	for _, c := range candles {
		d := s.Update(c, scale)
		if d.Signal() != Hold {
			out = append(out, d)
		}
	}
	return out
}

func flatCandles(scale int32, n int, price float64) []market.OHLC {
	cs := make([]market.OHLC, 0, n)
	for i := 0; i < n; i++ {
		// Completely flat OHLC => TR=0 => ADX should remain near 0 after ready.
		cs = append(cs, mkOHLC(scale, price, price, price, price))
	}
	return cs
}

func trendingCandles(scale int32, n int, start float64, step float64, halfRange float64) []market.OHLC {
	cs := make([]market.OHLC, 0, n)
	p := start
	for i := 0; i < n; i++ {
		o := p
		c := p + step
		h := c + halfRange
		l := o - halfRange
		cs = append(cs, mkOHLC(scale, o, h, l, c))
		p = c
	}
	return cs
}

// Builds a sequence that (1) warms up, (2) establishes a baseline BELOW (fast<slow),
// (3) trends up to cross above, (4) trends down to cross below.
func baselineThenCrossUpThenDownOHLC(scale int32) []market.OHLC {
	out := make([]market.OHLC, 0, 300)

	// Warmup flat
	out = append(out, flatCandles(scale, 60, 1.0000)...)

	// Establish baseline below: steady downtrend
	out = append(out, trendingCandles(scale, 40, 1.0000, -0.0002, 0.00005)...)

	// Strong uptrend: should cross up
	out = append(out, trendingCandles(scale, 60, 0.9920, +0.0003, 0.00005)...)

	// Strong downtrend: should cross down
	out = append(out, trendingCandles(scale, 60, 1.0100, -0.0003, 0.00005)...)

	return out
}

func TestEMACrossADX_FlatDoesNotSignalWhenADXThresholdHigh(t *testing.T) {
	scale := int32(10000)

	s := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    25.0,
		RequireDI:       false,
		RequireADXReady: true,
		MinSpread:       0,
	})

	candles := flatCandles(scale, 200, 1.2345)

	events := feedSignalsADX(s, scale, candles)
	require.Len(t, events, 0, "flat market should not pass ADX gate or produce cross signals")
}

func TestEMACrossADX_TrendAllowsSignals(t *testing.T) {
	scale := int32(10000)

	s := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    20.0,
		RequireDI:       false,
		RequireADXReady: true,
		MinSpread:       0,
	})

	candles := baselineThenCrossUpThenDownOHLC(scale)
	events := feedSignalsADX(s, scale, candles)

	// Expect at least BUY then SELL.
	require.GreaterOrEqual(t, len(events), 2, "expected at least BUY then SELL in a strong trend series")
	require.Equal(t, Buy, events[0].Signal(), "first signal should be BUY (cross up after baseline)")

	foundSell := false
	for i := 1; i < len(events); i++ {
		if events[i].Signal() == Sell {
			foundSell = true
			break
		}
	}
	require.True(t, foundSell, "expected a SELL after the BUY")
}

func TestEMACrossADX_DIConfirmationDoesNotIncreaseSignals(t *testing.T) {
	scale := int32(10000)

	candles := baselineThenCrossUpThenDownOHLC(scale)

	// No DI confirmation
	s0 := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    20.0,
		RequireDI:       false,
		RequireADXReady: true,
		MinSpread:       0,
	})
	events0 := feedSignalsADX(s0, scale, candles)

	// With DI confirmation
	s1 := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    20.0,
		RequireDI:       true,
		RequireADXReady: true,
		MinSpread:       0,
	})
	events1 := feedSignalsADX(s1, scale, candles)

	// DI confirmation is a filter: it should never create *more* signals.
	require.LessOrEqual(t, len(events1), len(events0))

	// (Optional sanity) Both strategies must only emit BUY/SELL (never HOLD here).
	for _, e := range events1 {
		require.True(t, e.Signal() == Buy || e.Signal() == Sell)
	}
}

func TestEMACrossADX_ResetReplaysSameSignals(t *testing.T) {
	scale := int32(10000)

	cfg := EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    20.0,
		RequireDI:       false,
		RequireADXReady: true,
		MinSpread:       0,
	}

	s := NewEMACrossADX(cfg)

	candles := baselineThenCrossUpThenDownOHLC(scale)

	events1 := feedSignalsADX(s, scale, candles)
	require.NotEmpty(t, events1)

	s.Reset()

	events2 := feedSignalsADX(s, scale, candles)
	require.Equal(t, events1, events2, "after reset, strategy should emit identical signals")
}

// Optional sanity check: strategy should not blow up if only closes are provided.
// (ADX needs H/L; if H/L are zero, it will behave oddly. This test just ensures no panic.)
func TestEMACrossADX_CloseOnlyCandles_NoPanic(t *testing.T) {
	scale := int32(10000)

	s := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       14,
		Scale:           scale,
		ADXThreshold:    20.0,
		RequireDI:       false,
		RequireADXReady: false,
		MinSpread:       0,
	})

	closes := []float64{1.0, 1.0, 1.0, 1.0001, 1.0002, 1.0003, 1.0002, 1.0001}
	var events []Decision
	require.NotPanics(t, func() {
		for _, v := range closes {
			d := s.Update(mkClose(scale, v), scale)
			if d.Signal() != Hold {
				events = append(events, d)
			}
		}
	})
	_ = events
}
