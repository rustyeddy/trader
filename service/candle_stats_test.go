package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildStatsStore writes one month of H1 EURUSD candles to a temp store.
func buildStatsStore(t *testing.T) func() {
	t.Helper()
	store := trader.NewStoreAt(t.TempDir())
	candles := make([]trader.Candle, 744) // January 2024 has 744 hours
	candles[0] = trader.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = trader.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	require.NoError(t, store.WriteMonthlyCandles("oanda", "EURUSD", trader.H1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles))
	return trader.SwapStore(store)
}

func TestCandleStats_ReturnsAllFourAnalyzers(t *testing.T) {
	restore := buildStatsStore(t)
	defer restore()

	result, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", result.Instrument)
	assert.Equal(t, "H1", result.Timeframe)
	assert.Equal(t, "2024-01-01", result.From)
	assert.Equal(t, "2024-01-31", result.To)
	assert.Len(t, result.Analyzers, 4)

	names := make([]string, len(result.Analyzers))
	for i, a := range result.Analyzers {
		names[i] = a.Name
	}
	assert.Contains(t, names, "Swing Range Distribution")
	assert.Contains(t, names, "Avg Spread Distribution")
	assert.Contains(t, names, "Trend Distribution")
	assert.Contains(t, names, "Session (by UTC hour)")
}

func TestCandleStats_DefaultsTimeframeToH1(t *testing.T) {
	restore := buildStatsStore(t)
	defer restore()

	result, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "EURUSD",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.NoError(t, err)
	assert.Equal(t, "H1", result.Timeframe)
}

func TestCandleStats_AllowsSingleDayRange(t *testing.T) {
	restore := buildStatsStore(t)
	defer restore()

	result, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-01",
	})
	require.NoError(t, err)
	assert.Equal(t, "2024-01-01", result.From)
	assert.Equal(t, "2024-01-01", result.To)
}

func TestCandleStats_NormalizesInstrument(t *testing.T) {
	restore := buildStatsStore(t)
	defer restore()

	result, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "EUR_USD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", result.Instrument)
}

func TestCandleStats_RejectsFromAfterTo(t *testing.T) {
	_, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-02-01",
		To:         "2024-01-01",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from must not be after to")
}

func TestCandleStats_RejectsUnknownInstrument(t *testing.T) {
	_, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "BOGUS",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.Error(t, err)
}

func TestCandleStats_RejectsBlankInstrument(t *testing.T) {
	_, err := (&Service{}).CandleStats(context.Background(), CandleStatsRequest{
		Instrument: "",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")
}
