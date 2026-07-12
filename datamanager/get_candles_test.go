package datamanager

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

// TestCandleWindowSeconds_MatchesReviewWindowRatio locks in the integer 7/5
// ratio (== 1.4x) against the equivalent float computation, and confirms
// rounding is always up, never short.
func TestCandleWindowSeconds_MatchesReviewWindowRatio(t *testing.T) {
	tests := []struct {
		tf    market.Timeframe
		count int
	}{
		{market.D1, 60},
		{market.H4, 60},
		{market.D1, 1},
		{market.H1, 37}, // odd count exercises the round-up path
	}
	for _, tt := range tests {
		got := candleWindowSeconds(tt.tf, tt.count)
		want := int64(float64(tt.tf) * float64(tt.count) * 1.4)
		require.GreaterOrEqual(t, got, want, "window must never be narrower than the 1.4x-equivalent span for tf=%v count=%d", tt.tf, tt.count)
	}
}

// TestGetCandles_CompactsWeekendGapAndTrimsToCount is the spec's acceptance
// check (docs/asof-review-sweep-spec.md §2): a CandleSet spanning a known
// weekend gap must never surface a zero-value market.Candle from a
// closed-market slot, and the result must never exceed count entries even
// though the underlying month has far more (mostly invalid) slots.
func TestGetCandles_CompactsWeekendGapAndTrimsToCount(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	// Friday close, then the weekend gap, then Monday open — D1 candles.
	fri := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC) // Friday
	mon := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC) // Monday
	tue := time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	wed := time.Date(2026, time.January, 7, 0, 0, 0, 0, time.UTC) // Wednesday

	writeMonthlyCandles(t, s, "EURUSD", market.D1, 2026, time.January, map[time.Time]market.Candle{
		fri: {Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
		mon: {Open: 103, High: 108, Low: 102, Close: 107, Ticks: 1},
		tue: {Open: 107, High: 110, Low: 106, Close: 109, Ticks: 1},
		wed: {Open: 109, High: 112, Low: 108, Close: 111, Ticks: 1},
	})

	req := CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range:      market.TimeRange{TF: market.D1}, // overwritten by GetCandles
	}

	candles, err := dm.GetCandles(context.Background(), req, wed, 2)
	require.NoError(t, err)
	require.Len(t, candles, 2, "must never return more than count entries")

	for _, ct := range candles {
		require.False(t, ct.Candle.IsZero(), "must never return a zero-value candle from a closed-market slot")
	}

	require.Equal(t, []market.Candle{
		{Open: 107, High: 110, Low: 106, Close: 109, Ticks: 1},
		{Open: 109, High: 112, Low: 108, Close: 111, Ticks: 1},
	}, candlesOnly(candles), "expected the 2 most recent valid candles at/before asof, in order")
	require.Equal(t, []market.Timestamp{market.FromTime(tue), market.FromTime(wed)}, timestampsOnly(candles))
}

func TestGetCandles_IncludesCandleAtExactlyAsof(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", market.D1, 2026, time.March, map[time.Time]market.Candle{
		day: {Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
	})

	candles, err := dm.GetCandles(context.Background(), CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range:      market.TimeRange{TF: market.D1},
	}, day, 5)
	require.NoError(t, err)
	require.Len(t, candles, 1, "the candle whose open time equals asof must be included")
}

func TestGetCandles_ExcludesCandlesAfterAsof(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	day1 := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", market.D1, 2026, time.March, map[time.Time]market.Candle{
		day1: {Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
		day2: {Open: 103, High: 108, Low: 102, Close: 107, Ticks: 1},
	})

	candles, err := dm.GetCandles(context.Background(), CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range:      market.TimeRange{TF: market.D1},
	}, day1, 5)
	require.NoError(t, err)
	require.Equal(t, []market.Candle{
		{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
	}, candlesOnly(candles), "a candle opening after asof must not be included")
}

// TestGetCandles_SkipsFlaggedValidButZeroValueCandle covers a corrupt/
// partial CSV row: the Valid bitset only reflects the on-disk flag byte
// (per ReadCSV), not whether the OHLC content is real, so a row can be
// flagged valid yet hold a zero-value candle. GetCandles must still skip
// it — per copilot review on PR #154, this was the behavior
// service/review.go's readCachedOandaCandleTimes had (via ct.Candle.IsZero())
// before this logic moved into GetCandles, and it must not get lost in the
// move: a short/corrupt cache needs to come back short of count so
// review's fallback-to-OANDA path still triggers, rather than silently
// returning unusable candles.
func TestGetCandles_SkipsFlaggedValidButZeroValueCandle(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	day1 := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.April, 2, 0, 0, 0, 0, time.UTC) // flagged valid, zero content
	day3 := time.Date(2026, time.April, 3, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", market.D1, 2026, time.April, map[time.Time]market.Candle{
		day1: {Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
		day2: {}, // AddCandle marks this valid regardless of content
		day3: {Open: 103, High: 108, Low: 102, Close: 107, Ticks: 1},
	})

	candles, err := dm.GetCandles(context.Background(), CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range:      market.TimeRange{TF: market.D1},
	}, day3, 5)
	require.NoError(t, err)
	require.Equal(t, []market.Candle{
		{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1},
		{Open: 103, High: 108, Low: 102, Close: 107, Ticks: 1},
	}, candlesOnly(candles), "the flagged-valid zero-value candle must be skipped, not returned as usable data")
}

func TestGetCandles_RejectsNonPositiveCount(t *testing.T) {
	dm := &DataManager{}
	_, err := dm.GetCandles(context.Background(), CandleRequest{
		Instrument: "EURUSD",
		Range:      market.TimeRange{TF: market.D1},
	}, time.Now(), 0)
	require.Error(t, err)
}

func candlesOnly(cts []market.CandleTime) []market.Candle {
	out := make([]market.Candle, len(cts))
	for i, ct := range cts {
		out[i] = ct.Candle
	}
	return out
}

func timestampsOnly(cts []market.CandleTime) []market.Timestamp {
	out := make([]market.Timestamp, len(cts))
	for i, ct := range cts {
		out[i] = ct.Timestamp
	}
	return out
}
