package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/brokers/oanda"
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

// TestDownloadOandaCandles_ExcludesCandleBeforeMonthStart guards against a
// regression where a daily candle timestamped a few hours before the
// requested month's calendar boundary (OANDA's D1 candles open at 21:00 UTC
// the previous day, e.g. "2026-04-30T21:00:00Z" representing the April 30
// session) got miscomputed to index 0 of May instead of being excluded.
// Go's integer division truncates toward zero, so a small negative
// (time - monthStart) delta produced index 0 rather than -1, silently
// duplicating the prior month's last candle into the new month's first slot.
func TestDownloadOandaCandles_ExcludesCandleBeforeMonthStart(t *testing.T) {
	restore := marketdata.SwapStore(marketdata.NewStoreAt(t.TempDir()))
	t.Cleanup(restore)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls > 1 {
			fmt.Fprint(w, `{"instrument":"XAU_USD","granularity":"D","candles":[]}`)
			return
		}
		// A real April-30 daily candle (timestamped 21:00 UTC the day
		// before, per OANDA's D1 convention) returned in response to a
		// May 1..31 fetch — must not land in May's canonical file.
		fmt.Fprint(w, `{"instrument":"XAU_USD","granularity":"D","candles":[
			{"complete":true,"time":"2026-04-30T21:00:00.000000000Z","volume":100,
			 "bid":{"o":"4628.94","h":"4660.34","l":"4560.01","c":"4612.40"},
			 "ask":{"o":"4630.09","h":"4660.83","l":"4560.58","c":"4615.34"}}
		]}`)
	}))
	defer srv.Close()

	svc := &Service{OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"}, Log: discardLogger()}

	result, err := svc.DownloadOandaCandles(context.Background(), DownloadOandaCandlesRequest{
		Instrument: "XAU_USD",
		Timeframe:  "D",
		From:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Zero(t, result.CandlesWritten, "the April-30 candle must not be counted as written into May")

	path := marketdata.GetStore().PathForMonthlyCandle(marketdata.Key{
		Kind: marketdata.KindCandle, Source: market.SourceOanda,
		Instrument: "XAUUSD", TF: market.D1, Year: 2026, Month: 5,
	})
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "461240000", "May 1 slot must stay zero, not the duplicated April-30 close price")
}
