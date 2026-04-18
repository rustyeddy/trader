package trader

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func p(v float64) Price        { return PriceFromFloat(v) }
func ts(t time.Time) Timestamp { return FromTime(t) }
func m(v float64) Money        { return MoneyFromFloat(v) }

func TestGetTrade(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	open := time.Date(2024, 4, 10, 9, 0, 0, 0, time.UTC)
	close := time.Date(2024, 4, 10, 15, 30, 0, 0, time.UTC)

	expectedTrade := TradeRecord{
		TradeID:    "T123",
		Instrument: "EUR_USD",
		Units:      1500,
		EntryPrice: p(1.08500),
		ExitPrice:  p(1.08750),
		OpenTime:   ts(open),
		CloseTime:  ts(close),
		RealizedPL: m(375.00),
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
	assert.Equal(t, expectedTrade.Units, actualTrade.Units)
	assert.Equal(t, expectedTrade.EntryPrice, actualTrade.EntryPrice)
	assert.Equal(t, expectedTrade.ExitPrice, actualTrade.ExitPrice)
	assert.Equal(t, expectedTrade.OpenTime, actualTrade.OpenTime)
	assert.Equal(t, expectedTrade.CloseTime, actualTrade.CloseTime)
	assert.Equal(t, expectedTrade.RealizedPL, actualTrade.RealizedPL)
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
			EntryPrice: p(1.08000),
			ExitPrice:  p(1.08200),
			OpenTime:   ts(time.Now()),
			CloseTime:  ts(time.Now()),
			RealizedPL: m(200.00),
			Reason:     "test1",
		},
		{
			TradeID:    "T002",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: p(1.25000),
			ExitPrice:  p(1.24800),
			OpenTime:   ts(time.Now()),
			CloseTime:  ts(time.Now()),
			RealizedPL: m(-100.00),
			Reason:     "test2",
		},
		{
			TradeID:    "T003",
			Instrument: "USD_JPY",
			Units:      2000,
			EntryPrice: p(150.00),
			ExitPrice:  p(150.50),
			OpenTime:   ts(time.Now()),
			CloseTime:  ts(time.Now()),
			RealizedPL: m(1000.00),
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
		assert.Equal(t, expected.RealizedPL, actual.RealizedPL)
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
			EntryPrice: p(1.08000),
			ExitPrice:  p(1.08100),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(1 * time.Hour)), // May 1, 01:00
			RealizedPL: m(100.00),
			Reason:     "early",
		},
		{
			TradeID:    "T2",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: p(1.25000),
			ExitPrice:  p(1.25200),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(5 * time.Hour)), // May 1, 05:00
			RealizedPL: m(100.00),
			Reason:     "middle",
		},
		{
			TradeID:    "T3",
			Instrument: "USD_JPY",
			Units:      2000,
			EntryPrice: p(150.00),
			ExitPrice:  p(150.25),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(10 * time.Hour)), // May 1, 10:00
			RealizedPL: m(500.00),
			Reason:     "late",
		},
		{
			TradeID:    "T4",
			Instrument: "AUD_USD",
			Units:      750,
			EntryPrice: p(0.65000),
			ExitPrice:  p(0.65100),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(24 * time.Hour)), // May 2, 00:00
			RealizedPL: m(75.00),
			Reason:     "next_day",
		},
	}

	for _, trade := range trades {
		require.NoError(t, j.RecordTrade(trade))
	}

	// Test: Query for trades between 03:00 and 12:00 on May 1
	start := baseTime.Add(3 * time.Hour)
	end := baseTime.Add(12 * time.Hour)

	results, err := j.ListTradesClosedBetween(ts(start), ts(end))
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
			EntryPrice: p(150.00),
			ExitPrice:  p(150.25),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(10 * time.Hour)),
			RealizedPL: m(500.00),
			Reason:     "late",
		},
		{
			TradeID:    "T1",
			Instrument: "EUR_USD",
			Units:      1000,
			EntryPrice: p(1.08000),
			ExitPrice:  p(1.08100),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(2 * time.Hour)),
			RealizedPL: m(100.00),
			Reason:     "early",
		},
		{
			TradeID:    "T2",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: p(1.25000),
			ExitPrice:  p(1.25200),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(5 * time.Hour)),
			RealizedPL: m(100.00),
			Reason:     "middle",
		},
	}

	for _, trade := range trades {
		require.NoError(t, j.RecordTrade(trade))
	}

	// Query all trades
	start := baseTime
	end := baseTime.Add(24 * time.Hour)

	results, err := j.ListTradesClosedBetween(ts(start), ts(end))
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Verify they are ordered by close time (ascending)
	assert.Equal(t, "T1", results[0].TradeID)
	assert.Equal(t, "T2", results[1].TradeID)
	assert.Equal(t, "T3", results[2].TradeID)

	// Verify times are in ascending order
	assert.True(t, results[0].CloseTime < results[1].CloseTime)
	assert.True(t, results[1].CloseTime < results[2].CloseTime)
}

func TestListTradesClosedBetweenEmpty(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	// Query with no trades in database
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	results, err := j.ListTradesClosedBetween(ts(start), ts(end))
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
		EntryPrice: p(1.08000),
		ExitPrice:  p(1.08100),
		OpenTime:   ts(time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)),
		CloseTime:  ts(time.Date(2024, 5, 1, 15, 0, 0, 0, time.UTC)),
		RealizedPL: m(100.00),
		Reason:     "test",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query for a different time range
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	results, err := j.ListTradesClosedBetween(ts(start), ts(end))
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
		EntryPrice: p(1.08000),
		ExitPrice:  p(1.08100),
		OpenTime:   ts(baseTime),
		CloseTime:  ts(baseTime),
		RealizedPL: m(100.00),
		Reason:     "boundary",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query with start exactly at trade close time (should be included)
	results, err := j.ListTradesClosedBetween(ts(baseTime), ts(baseTime.Add(1*time.Hour)))
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
		EntryPrice: p(1.08000),
		ExitPrice:  p(1.08100),
		OpenTime:   ts(baseTime),
		CloseTime:  ts(baseTime),
		RealizedPL: m(100.00),
		Reason:     "boundary",
	}

	require.NoError(t, j.RecordTrade(trade))

	// Query with end exactly at trade close time (should be excluded)
	results, err := j.ListTradesClosedBetween(ts(baseTime.Add(-1*time.Hour)), ts(baseTime))
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
			TradeID:    fmt.Sprintf("T%d", i+1),
			Instrument: instrument,
			Units:      1000,
			EntryPrice: p(1.0),
			ExitPrice:  p(1.1),
			OpenTime:   ts(baseTime),
			CloseTime:  ts(baseTime.Add(time.Duration(i) * time.Hour)),
			RealizedPL: m(100.00),
			Reason:     "test",
		}
		require.NoError(t, j.RecordTrade(trade))
	}

	// Query all trades
	start := baseTime
	end := baseTime.Add(24 * time.Hour)

	results, err := j.ListTradesClosedBetween(ts(start), ts(end))
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

func TestListEquityBetweenBoundaryBehavior(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	base := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)
	recs := []EquitySnapshot{
		{
			Timestamp:   ts(base),
			Balance:     m(1000),
			Equity:      m(1000),
			MarginUsed:  m(10),
			FreeMargin:  m(990),
			MarginLevel: m(100),
		},
		{
			Timestamp:   ts(base.Add(1 * time.Hour)),
			Balance:     m(1010),
			Equity:      m(1008),
			MarginUsed:  m(12),
			FreeMargin:  m(996),
			MarginLevel: m(84),
		},
		{
			Timestamp:   ts(base.Add(2 * time.Hour)),
			Balance:     m(1020),
			Equity:      m(1015),
			MarginUsed:  m(15),
			FreeMargin:  m(1000),
			MarginLevel: m(67),
		},
	}
	for _, rec := range recs {
		require.NoError(t, j.RecordEquity(rec))
	}

	results, err := j.ListEquityBetween(base.Add(1*time.Hour), base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, results, 1, "start boundary should be inclusive and end boundary should be exclusive")
	assert.Equal(t, recs[1], results[0])
}

func TestListEquityBetweenEmptyResults(t *testing.T) {
	t.Parallel()

	j, _ := newTestSQLite(t)
	defer j.Close()

	base := time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC)

	results, err := j.ListEquityBetween(base, base.Add(1*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, results)

	require.NoError(t, j.RecordEquity(EquitySnapshot{
		Timestamp:   ts(base.Add(3 * time.Hour)),
		Balance:     m(1100),
		Equity:      m(1090),
		MarginUsed:  m(20),
		FreeMargin:  m(1070),
		MarginLevel: m(55),
	}))

	results, err = j.ListEquityBetween(base, base.Add(1*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, results)
}
