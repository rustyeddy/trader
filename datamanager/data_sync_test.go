package datamanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestCandleCSV creates a minimal monthly candle CSV for testing.
// nonZeroDays lists days in the month that should have real (flag=0x0001) candles.
func writeTestCandleCSV(t *testing.T, path string, year, month int, nonZeroDays []int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	daySet := make(map[int]bool)
	for _, d := range nonZeroDays {
		daySet[d] = true
	}

	// Write every hour of the month; mark listed days as real.
	for d := 1; d <= 28; d++ {
		ts := start.AddDate(0, 0, d-1).Unix()
		flag := "0x0000"
		if daySet[d] {
			flag = "0x0001"
		}
		fmt.Fprintf(f, "%d,110000,110100,109900,110050,10,15,100,%s\n", ts, flag)
	}
}

func TestLastCompleteDate_FindsLastRealRow(t *testing.T) {
	UseTempDataDir(t)

	k := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H1,
		Year:       2026,
		Month:      5,
	}
	writeTestCandleCSV(t, PathForMonthlyCandle(k), 2026, 5, []int{1, 15, 20})

	dm := NewDataManager([]string{"EURUSD"}, time.Now(), time.Now())
	got, err := dm.LastCompleteDate("EUR_USD", types.H1, market.SourceOanda)
	require.NoError(t, err)

	want := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, want, got)
}

func TestLastCompleteDate_NoFilesError(t *testing.T) {
	UseTempDataDir(t)

	dm := NewDataManager([]string{"EURUSD"}, time.Now(), time.Now())
	_, err := dm.LastCompleteDate("EUR_USD", types.H1, market.SourceOanda)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no candle files found")
}

func TestLastCompleteDate_AllZerosError(t *testing.T) {
	UseTempDataDir(t)

	k := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H1,
		Year:       2026,
		Month:      5,
	}
	writeTestCandleCSV(t, PathForMonthlyCandle(k), 2026, 5, nil) // no real candles

	dm := NewDataManager([]string{"EURUSD"}, time.Now(), time.Now())
	_, err := dm.LastCompleteDate("EUR_USD", types.H1, market.SourceOanda)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no non-zero candles")
}

// TestDeriveCanonicalFromRaw_UsesTrueObservedTimestamps proves the
// canonical file's timestamps come from the raw rows' own observed times,
// not a naive assumption that slot 0 begins at UTC midnight. This is the
// regression case for the H4/D1 timestamp-mislabeling bug: OANDA's real
// daily-alignment grid (17:00 America/New_York, DST-dependent) does not
// begin at UTC midnight — June is EDT (UTC-4), so the true boundary is
// 21:00 UTC.
func TestDeriveCanonicalFromRaw_UsesTrueObservedTimestamps(t *testing.T) {
	rawDir := t.TempDir()
	UseTempDataDir(t)

	key := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H4,
		Year:       2026,
		Month:      6,
	}

	monthStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	trueFirstSlot := MonthSlotBoundaries(monthStart, monthEnd, types.H4)[0]

	var rows []RawCandleRow
	for i := 0; i < 6; i++ {
		rows = append(rows, RawCandleRow{
			Time:    trueFirstSlot.Add(time.Duration(i) * 4 * time.Hour),
			BidOpen: 1.1000, BidHigh: 1.1010, BidLow: 1.0990, BidClose: 1.1005,
			AskOpen: 1.1002, AskHigh: 1.1012, AskLow: 1.0992, AskClose: 1.1007,
			Volume:   100,
			Complete: true,
		})
	}
	require.NoError(t, writeRawMonth(rawDir, key, monthStart, rows))
	rawPath := monthlyCandle(rawDir, key)

	dm := NewDataManager([]string{"EURUSD"}, monthStart, monthStart.AddDate(0, 1, 0))
	result, err := dm.DeriveCanonicalFromRaw(context.Background(), rawPath, key)
	require.NoError(t, err)
	require.Equal(t, 6, result.CandlesWritten)

	cs, err := getStore().ReadCSV(key)
	require.NoError(t, err)

	assert.Equal(t, types.FromTime(trueFirstSlot), cs.Start,
		"canonical Start must be the raw data's true observed first-slot time, not UTC midnight")

	for i := 0; i < 6; i++ {
		require.True(t, cs.IsValid(i), "slot %d should be valid", i)
		wantTime := trueFirstSlot.Add(time.Duration(i) * 4 * time.Hour)
		assert.Equal(t, wantTime, cs.Time(i), "slot %d timestamp", i)
	}
}

// TestDeriveCanonicalFromRaw_D1DSTTransition proves a full raw D1 month
// spanning the US spring-forward transition (2026-03-08) derives cleanly:
// every real day's row lands in its own slot (no collisions from the 23h
// transition day), and each slot's canonical timestamp matches the true
// daily-alignment boundary, not a naive fixed-86400s reconstruction.
func TestDeriveCanonicalFromRaw_D1DSTTransition(t *testing.T) {
	rawDir := t.TempDir()
	UseTempDataDir(t)

	key := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.D1,
		Year:       2026,
		Month:      3,
	}

	monthStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	boundaries := SlotBoundaries(monthStart, types.D1, 31)

	var rows []RawCandleRow
	for _, b := range boundaries {
		rows = append(rows, RawCandleRow{
			Time:    b,
			BidOpen: 1.1000, BidHigh: 1.1010, BidLow: 1.0990, BidClose: 1.1005,
			AskOpen: 1.1002, AskHigh: 1.1012, AskLow: 1.0992, AskClose: 1.1007,
			Volume:   100,
			Complete: true,
		})
	}
	require.NoError(t, writeRawMonth(rawDir, key, monthStart, rows))
	rawPath := monthlyCandle(rawDir, key)

	dm := NewDataManager([]string{"EURUSD"}, monthStart, monthStart.AddDate(0, 1, 0))
	result, err := dm.DeriveCanonicalFromRaw(context.Background(), rawPath, key)
	require.NoError(t, err)
	// CandlesWritten/MissingSlots only tally market-hours slots (weekends
	// are excluded), so they're not 31 here; the collision-free placement
	// claim is verified directly against every one of the 31 written
	// slots below instead.
	require.Zero(t, result.MissingSlots)

	cs, err := getStore().ReadCSV(key)
	require.NoError(t, err)
	require.Equal(t, 31, cs.CountValid())
	for i, b := range boundaries {
		require.True(t, cs.IsValid(i), "slot %d should be valid", i)
		require.Equal(t, b, cs.Time(i), "slot %d timestamp", i)
	}
}

func TestLastCompleteDate_PicksNewestMonth(t *testing.T) {
	UseTempDataDir(t)

	makeKey := func(month int) Key {
		return Key{
			Kind: KindCandle, Source: market.SourceOanda,
			Instrument: "EURUSD", TF: types.H1, Year: 2026, Month: month,
		}
	}
	// Write March (day 10) and May (day 5) — should pick May.
	writeTestCandleCSV(t, PathForMonthlyCandle(makeKey(3)), 2026, 3, []int{10})
	writeTestCandleCSV(t, PathForMonthlyCandle(makeKey(5)), 2026, 5, []int{5})

	dm := NewDataManager([]string{"EURUSD"}, time.Now(), time.Now())
	got, err := dm.LastCompleteDate("EUR_USD", types.H1, market.SourceOanda)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), got)
}
