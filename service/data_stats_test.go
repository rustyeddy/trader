package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedStatsStore writes one month of H1 EURUSD candles to a temp store.
func seedStatsStore(t *testing.T) {
	t.Helper()
	candles := make([]market.Candle, 744) // January 2024 has 744 hours
	candles[0] = market.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = market.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	datamanager.SeedCandles(t, "oanda", "EURUSD", market.H1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles)
}

func TestDataStats_ReturnsAllFourAnalyzers(t *testing.T) {
	seedStatsStore(t)

	result, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
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

func TestDataStats_DefaultsTimeframeToH1(t *testing.T) {
	seedStatsStore(t)

	result, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "EURUSD",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.NoError(t, err)
	assert.Equal(t, "H1", result.Timeframe)
}

func TestDataStats_AllowsSingleDayRange(t *testing.T) {
	seedStatsStore(t)

	result, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-01",
	})
	require.NoError(t, err)
	assert.Equal(t, "2024-01-01", result.From)
	assert.Equal(t, "2024-01-01", result.To)
}

func TestDataStats_NormalizesInstrument(t *testing.T) {
	seedStatsStore(t)

	result, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "EUR_USD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", result.Instrument)
}

func TestDataStats_RejectsFromAfterTo(t *testing.T) {
	_, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-02-01",
		To:         "2024-01-01",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from must not be after to")
}

func TestDataStats_RejectsUnknownInstrument(t *testing.T) {
	_, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "BOGUS",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.Error(t, err)
}

func TestDataStats_RejectsBlankInstrument(t *testing.T) {
	_, err := (&Service{}).DataStats(context.Background(), DataStatsRequest{
		Instrument: "",
		From:       "2024-01-01",
		To:         "2024-01-31",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")
}
