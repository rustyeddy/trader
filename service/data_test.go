package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
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

// TestDownloadOandaCandles_ExcludesCandleBeforeMonthStart guards against a
// regression where a daily candle timestamped a few hours before the
// requested month's calendar boundary (OANDA's D1 candles open at 21:00 UTC
// the previous day, e.g. "2026-04-30T21:00:00Z" representing the April 30
// session) got miscomputed to index 0 of May instead of being excluded.
// Go's integer division truncates toward zero, so a small negative
// (time - monthStart) delta produced index 0 rather than -1, silently
// duplicating the prior month's last candle into the new month's first slot.
func TestDownloadOandaCandles_ExcludesCandleBeforeMonthStart(t *testing.T) {
	datamanager.UseTempDataDir(t)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls > 1 {
			fmt.Fprint(w, `{"instrument":"EUR_USD","granularity":"D","candles":[]}`)
			return
		}
		// A real April-30 daily candle (timestamped 21:00 UTC the day
		// before, per OANDA's D1 convention) returned in response to a
		// May 1..31 fetch — must not land in May's canonical file.
		fmt.Fprint(w, `{"instrument":"EUR_USD","granularity":"D","candles":[
			{"complete":true,"time":"2026-04-30T21:00:00.000000000Z","volume":100,
			 "bid":{"o":"1.08294","h":"1.08334","l":"1.08201","c":"1.08240"},
			 "ask":{"o":"1.08309","h":"1.08347","l":"1.08216","c":"1.08253"}}
		]}`)
	}))
	defer srv.Close()

	svc := &Service{OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"}, Log: discardLogger()}

	result, err := svc.DownloadOandaCandles(context.Background(), DownloadOandaCandlesRequest{
		Instrument: "EUR_USD",
		Timeframe:  "D",
		From:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Zero(t, result.CandlesWritten, "the April-30 candle must not be counted as written into May")

	path := datamanager.PathForMonthlyCandle(datamanager.Key{
		Kind: datamanager.KindCandle, Source: market.SourceOanda,
		Instrument: "EURUSD", TF: types.D1, Year: 2026, Month: 5,
	})
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "108240", "May 1 slot must stay zero, not the duplicated April-30 close price")
}
