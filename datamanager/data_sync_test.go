package datamanager

import (
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
