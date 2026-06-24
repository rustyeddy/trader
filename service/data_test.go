package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
)

func TestToOandaGranularity(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"D1", "D"},
		{"d1", "D"},
		{"D", "D"},
		{"d", "D"},
		{"H1", "H1"},
		{"h1", "H1"},
		{"H4", "H4"},
		{"h4", "H4"},
		{"M1", "M1"},
		{"m1", "M1"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, toOandaGranularity(tc.in), "input %q", tc.in)
	}
}

func TestParseTraderTimeframe(t *testing.T) {
	_, err := parseTraderTimeframe("D1")
	assert.NoError(t, err)

	_, err = parseTraderTimeframe("D")
	assert.NoError(t, err)

	_, err = parseTraderTimeframe("H4")
	assert.NoError(t, err)

	_, err = parseTraderTimeframe("bogus")
	assert.Error(t, err)
}

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

func TestLastNonZeroCandleDate_FindsLastRealRow(t *testing.T) {
	dir := t.TempDir()
	restore := marketdata.SwapStore(marketdata.NewStoreAt(dir))
	t.Cleanup(restore)

	store := marketdata.GetStore()
	k := marketdata.Key{
		Kind:       marketdata.KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         market.H1,
		Year:       2026,
		Month:      5,
	}
	path := store.PathForMonthlyCandle(k)
	writeTestCandleCSV(t, path, 2026, 5, []int{1, 15, 20})

	got, err := lastNonZeroCandleDate(marketdata.GetStore(), "EUR_USD", "H1")
	require.NoError(t, err)

	want := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, want, got)
}

func TestLastNonZeroCandleDate_NoFilesError(t *testing.T) {
	dir := t.TempDir()
	restore := marketdata.SwapStore(marketdata.NewStoreAt(dir))
	t.Cleanup(restore)

	_, err := lastNonZeroCandleDate(marketdata.GetStore(), "EUR_USD", "H1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no candle files found")
}

func TestLastNonZeroCandleDate_AllZerosError(t *testing.T) {
	dir := t.TempDir()
	restore := marketdata.SwapStore(marketdata.NewStoreAt(dir))
	t.Cleanup(restore)

	store := marketdata.GetStore()
	k := marketdata.Key{
		Kind:       marketdata.KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         market.H1,
		Year:       2026,
		Month:      5,
	}
	writeTestCandleCSV(t, store.PathForMonthlyCandle(k), 2026, 5, nil) // no real candles

	_, err := lastNonZeroCandleDate(marketdata.GetStore(), "EUR_USD", "H1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no non-zero candles")
}

func TestLastNonZeroCandleDate_PicksNewestMonth(t *testing.T) {
	dir := t.TempDir()
	restore := marketdata.SwapStore(marketdata.NewStoreAt(dir))
	t.Cleanup(restore)

	store := marketdata.GetStore()
	makeKey := func(month int) marketdata.Key {
		return marketdata.Key{
			Kind: marketdata.KindCandle, Source: market.SourceOanda,
			Instrument: "EURUSD", TF: market.H1, Year: 2026, Month: month,
		}
	}
	// Write March (day 10) and May (day 5) — should pick May.
	writeTestCandleCSV(t, store.PathForMonthlyCandle(makeKey(3)), 2026, 3, []int{10})
	writeTestCandleCSV(t, store.PathForMonthlyCandle(makeKey(5)), 2026, 5, []int{5})

	got, err := lastNonZeroCandleDate(marketdata.GetStore(), "EUR_USD", "H1")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), got)
}

func TestLastNonZeroDate_NormalizesToUTCDate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candles.csv")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	_, err = fmt.Fprintln(f, "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags")
	require.NoError(t, err)
	_, err = fmt.Fprintln(f, "1747353599,110000,110100,109900,110050,10,15,100,0x0001") // 2025-05-15T23:59:59Z
	require.NoError(t, err)

	got, err := lastNonZeroDate(path)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC), got)
}
