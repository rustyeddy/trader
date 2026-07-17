package datamanager

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func buildAggTestCandleSet(t *testing.T, start time.Time, tf types.Timeframe, n int) *CandleSet {
	t.Helper()

	boundaries := SlotBoundaries(start, tf, n)
	candles := make([]market.CandleTime, n)
	for i, b := range boundaries {
		candles[i] = market.CandleTime{
			Candle:    market.Candle{Open: 1, High: 2, Low: 1, Close: 1, Ticks: 1},
			Timestamp: types.FromTime(b),
		}
	}

	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      types.FromTime(start),
		Timeframe:  tf,
		Scale:      types.PriceScale,
		Source:     market.SourceOanda,
		Candles:    candles,
		Valid:      make([]uint64, (n+63)/64),
	}
	for i := 0; i < n; i++ {
		types.BitSet(cs.Valid, i)
	}
	return cs
}

// TestAggregate_D1FromH1_UsesNYDailyAlignment is the regression case for
// root cause 2 (#179): D1 built locally from H1 (buildD1 -> Aggregate)
// must bucket days at OANDA's true 17:00 America/New_York daily-alignment
// boundary, not at UTC midnight.
func TestAggregate_D1FromH1_UsesNYDailyAlignment(t *testing.T) {
	t.Parallel()

	// June is EDT (UTC-4): the true daily boundary is 21:00 UTC, not
	// 00:00 UTC.
	start := time.Date(2026, 6, 1, 21, 0, 0, 0, time.UTC)
	cs := buildAggTestCandleSet(t, start, types.H1, 48) // two full days

	d1, err := cs.Aggregate(types.D1)
	require.NoError(t, err)

	require.Equal(t, start, d1.Time(0))
	require.Equal(t, start.Add(24*time.Hour), d1.Time(1))
	require.True(t, d1.IsValid(0))
	require.True(t, d1.IsValid(1))
}

// TestAggregate_D1FromH1_DSTTransition proves the D1 day boundary shifts
// correctly across a DST transition instead of using a fixed offset — the
// exact case a naive UTC-epoch floor (or a hardcoded-offset "fix") gets
// wrong. 2026-03-08 02:00 America/New_York is when US clocks spring
// forward (EST -> EDT): 2026-03-07's boundary is 22:00 UTC (EST), but
// 2026-03-08's boundary is 21:00 UTC (EDT).
func TestAggregate_D1FromH1_DSTTransition(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 7, 22, 0, 0, 0, time.UTC) // true Mar 7 boundary (EST)
	cs := buildAggTestCandleSet(t, start, types.H1, 72)   // spans Mar 7, 8, 9

	d1, err := cs.Aggregate(types.D1)
	require.NoError(t, err)

	require.Equal(t, start, d1.Time(0), "pre-transition day boundary should stay 22:00 UTC (EST)")
	require.Equal(t, time.Date(2026, 3, 8, 21, 0, 0, 0, time.UTC), d1.Time(1),
		"post-transition day boundary should shift to 21:00 UTC (EDT)")
	require.Equal(t, time.Date(2026, 3, 9, 21, 0, 0, 0, time.UTC), d1.Time(2))
}

// TestAggregate_H1FromM1_UnaffectedByDailyAlignmentFix proves buildH1
// (M1->H1) stays a no-op change from the D1 daily-alignment fix: M1 has no
// daily-alignment concept, so H1 bucket boundaries are true UTC-epoch-hour
// boundaries regardless of the broker's NY-anchored trading day.
func TestAggregate_H1FromM1_UnaffectedByDailyAlignmentFix(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cs := buildAggTestCandleSet(t, start, types.M1, 120)

	h1, err := cs.Aggregate(types.H1)
	require.NoError(t, err)
	require.Equal(t, cs.Start, h1.Start)
}
