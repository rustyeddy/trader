package journal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTrade(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	open := time.Date(2024, 4, 10, 9, 0, 0, 0, time.UTC)
	close := time.Date(2024, 4, 10, 15, 30, 0, 0, time.UTC)

	expectedTrade := TradeRecord{
		TradeID:    "T123",
		Instrument: "EUR_USD",
		Units:      1500.0,
		EntryPrice: 1.08500,
		ExitPrice:  1.08750,
		OpenTime:   open,
		CloseTime:  close,
		RealizedPL: 375.00,
		Reason:     "trend",
	}

	// Insert the trade
	err := j.RecordTrade(expectedTrade)
	require.NoError(t, err)

	// Retrieve the trade
	actualTrade, err := j.GetTrade("T123")
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, expectedTrade.TradeID, actualTrade.TradeID)
	assert.Equal(t, expectedTrade.Instrument, actualTrade.Instrument)
	assert.InDelta(t, expectedTrade.Units, actualTrade.Units, 1e-6)
	assert.InDelta(t, expectedTrade.EntryPrice, actualTrade.EntryPrice, 1e-9)
	assert.InDelta(t, expectedTrade.ExitPrice, actualTrade.ExitPrice, 1e-9)
	assert.True(t, actualTrade.OpenTime.Equal(expectedTrade.OpenTime))
	assert.True(t, actualTrade.CloseTime.Equal(expectedTrade.CloseTime))
	assert.InDelta(t, expectedTrade.RealizedPL, actualTrade.RealizedPL, 1e-6)
	assert.Equal(t, expectedTrade.Reason, actualTrade.Reason)
}

func TestGetTradeNotFound(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	_, err := j.GetTrade("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetTradeMultipleTrades(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	// Insert multiple trades
	trades := []TradeRecord{
		{
			TradeID:    "T001",
			Instrument: "EUR_USD",
			Units:      1000,
			EntryPrice: 1.08000,
			ExitPrice:  1.08200,
			OpenTime:   time.Now(),
			CloseTime:  time.Now(),
			RealizedPL: 200.00,
			Reason:     "test1",
		},
		{
			TradeID:    "T002",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: 1.25000,
			ExitPrice:  1.24800,
			OpenTime:   time.Now(),
			CloseTime:  time.Now(),
			RealizedPL: -100.00,
			Reason:     "test2",
		},
		{
			TradeID:    "T003",
			Instrument: "USD_JPY",
			Units:      2000,
			EntryPrice: 150.00,
			ExitPrice:  150.50,
			OpenTime:   time.Now(),
			CloseTime:  time.Now(),
			RealizedPL: 1000.00,
			Reason:     "test3",
		},
	}

	for _, trade := range trades {
		require.NoError(t, j.RecordTrade(trade))
	}

	// Retrieve each trade and verify
	for _, expected := range trades {
		actual, err := j.GetTrade(expected.TradeID)
		require.NoError(t, err)
		assert.Equal(t, expected.TradeID, actual.TradeID)
		assert.Equal(t, expected.Instrument, actual.Instrument)
		assert.InDelta(t, expected.RealizedPL, actual.RealizedPL, 1e-6)
	}
}

func TestListTradesClosedBetween(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	// Create trades with different close times
	baseTime := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	trades := []TradeRecord{
		{
			TradeID:    "T1",
			Instrument: "EUR_USD",
			Units:      1000,
			EntryPrice: 1.08000,
			ExitPrice:  1.08100,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(1 * time.Hour), // May 1, 01:00
			RealizedPL: 100.00,
			Reason:     "early",
		},
		{
			TradeID:    "T2",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: 1.25000,
			ExitPrice:  1.25200,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(5 * time.Hour), // May 1, 05:00
			RealizedPL: 100.00,
			Reason:     "middle",
		},
		{
			TradeID:    "T3",
			Instrument: "USD_JPY",
			Units:      2000,
			EntryPrice: 150.00,
			ExitPrice:  150.25,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(10 * time.Hour), // May 1, 10:00
			RealizedPL: 500.00,
			Reason:     "late",
		},
		{
			TradeID:    "T4",
			Instrument: "AUD_USD",
			Units:      750,
			EntryPrice: 0.65000,
			ExitPrice:  0.65100,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(24 * time.Hour), // May 2, 00:00
			RealizedPL: 75.00,
			Reason:     "next_day",
		},
	}

	for _, trade := range trades {
		require.NoError(t, j.RecordTrade(trade))
	}

	// Test: Query for trades between 03:00 and 12:00 on May 1
	start := baseTime.Add(3 * time.Hour)
	end := baseTime.Add(12 * time.Hour)

	results, err := j.ListTradesClosedBetween(start, end)
	require.NoError(t, err)
	require.Len(t, results, 2, "Expected 2 trades in the time range")

	// Verify the correct trades were returned
	assert.Equal(t, "T2", results[0].TradeID)
	assert.Equal(t, "T3", results[1].TradeID)
}

func TestListTradesClosedBetweenOrdering(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	baseTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Insert trades in non-chronological order
	trades := []TradeRecord{
		{
			TradeID:    "T3",
			Instrument: "USD_JPY",
			Units:      2000,
			EntryPrice: 150.00,
			ExitPrice:  150.25,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(10 * time.Hour),
			RealizedPL: 500.00,
			Reason:     "late",
		},
		{
			TradeID:    "T1",
			Instrument: "EUR_USD",
			Units:      1000,
			EntryPrice: 1.08000,
			ExitPrice:  1.08100,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(2 * time.Hour),
			RealizedPL: 100.00,
			Reason:     "early",
		},
		{
			TradeID:    "T2",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: 1.25000,
			ExitPrice:  1.25200,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(5 * time.Hour),
			RealizedPL: 100.00,
			Reason:     "middle",
		},
	}

	for _, trade := range trades {
		require.NoError(t, j.RecordTrade(trade))
	}

	// Query all trades
	start := baseTime
	end := baseTime.Add(24 * time.Hour)

	results, err := j.ListTradesClosedBetween(start, end)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Verify they are ordered by close time (ascending)
	assert.Equal(t, "T1", results[0].TradeID)
	assert.Equal(t, "T2", results[1].TradeID)
	assert.Equal(t, "T3", results[2].TradeID)

	// Verify times are in ascending order
	assert.True(t, results[0].CloseTime.Before(results[1].CloseTime))
	assert.True(t, results[1].CloseTime.Before(results[2].CloseTime))
}

func TestListTradesClosedBetweenEmpty(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	// Query with no trades in database
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	results, err := j.ListTradesClosedBetween(start, end)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestListTradesClosedBetweenNoMatches(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	// Insert a trade
	trade := TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      1000,
		EntryPrice: 1.08000,
		ExitPrice:  1.08100,
		OpenTime:   time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC),
		CloseTime:  time.Date(2024, 5, 1, 15, 0, 0, 0, time.UTC),
		RealizedPL: 100.00,
		Reason:     "test",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query for a different time range
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	results, err := j.ListTradesClosedBetween(start, end)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestListTradesClosedBetweenBoundaryInclusive(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	baseTime := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)

	trade := TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      1000,
		EntryPrice: 1.08000,
		ExitPrice:  1.08100,
		OpenTime:   baseTime,
		CloseTime:  baseTime,
		RealizedPL: 100.00,
		Reason:     "boundary",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query with start exactly at trade close time (should be included)
	results, err := j.ListTradesClosedBetween(baseTime, baseTime.Add(1*time.Hour))
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "T1", results[0].TradeID)
}

func TestListTradesClosedBetweenBoundaryExclusive(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	baseTime := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)

	trade := TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      1000,
		EntryPrice: 1.08000,
		ExitPrice:  1.08100,
		OpenTime:   baseTime,
		CloseTime:  baseTime,
		RealizedPL: 100.00,
		Reason:     "boundary",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query with end exactly at trade close time (should be excluded)
	results, err := j.ListTradesClosedBetween(baseTime.Add(-1*time.Hour), baseTime)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestListTradesClosedBetweenMultipleInstruments(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	baseTime := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)

	// Insert trades for different instruments
	instruments := []string{"EUR_USD", "GBP_USD", "USD_JPY", "AUD_USD", "USD_CAD"}
	for i, instrument := range instruments {
		trade := TradeRecord{
			TradeID:    "T" + string(rune('1'+i)),
			Instrument: instrument,
			Units:      1000,
			EntryPrice: 1.0,
			ExitPrice:  1.1,
			OpenTime:   baseTime,
			CloseTime:  baseTime.Add(time.Duration(i) * time.Hour),
			RealizedPL: 100.00,
			Reason:     "test",
		}
		require.NoError(t, j.RecordTrade(trade))
	}

	// Query all trades
	start := baseTime
	end := baseTime.Add(24 * time.Hour)

	results, err := j.ListTradesClosedBetween(start, end)
	require.NoError(t, err)
	require.Len(t, results, len(instruments))

	// Verify all instruments are represented
	instrumentSet := make(map[string]bool)
	for _, result := range results {
		instrumentSet[result.Instrument] = true
	}

	for _, instrument := range instruments {
		assert.True(t, instrumentSet[instrument], "Expected instrument %s to be in results", instrument)
	}
}
