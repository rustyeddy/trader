package oanda_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oandaclient "github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	provider "github.com/rustyeddy/trader/datamanager/oanda"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// fakeCandlesServer serves OANDA's price=BA candle response shape for a
// fixed set of times.
func fakeCandlesServer(t *testing.T, times []time.Time) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type ohlc struct{ O, H, L, C string }
		candles := make([]map[string]any, 0, len(times))
		price := 1.10000
		for _, ts := range times {
			open := price
			price += 0.0001
			bid := ohlc{
				O: fmt.Sprintf("%.5f", open),
				H: fmt.Sprintf("%.5f", price+0.00005),
				L: fmt.Sprintf("%.5f", open-0.00005),
				C: fmt.Sprintf("%.5f", price),
			}
			candles = append(candles, map[string]any{
				"complete": true,
				"time":     ts.UTC().Format(time.RFC3339Nano),
				"volume":   10,
				"bid":      bid,
				"ask":      bid,
			})
		}
		resp := map[string]any{
			"instrument":  "EUR_USD",
			"granularity": r.URL.Query().Get("granularity"),
			"candles":     candles,
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

// TestFetchCandleMonth_H4_PlacesTrueTimestamps is the regression case for
// root cause 1's live-download duplicate (#179): FetchCandleMonth must
// place each candle at its own true observed timestamp — not a naive
// UTC-midnight-relative reconstruction — and that timestamp must survive
// into CandleMonth.Candles verbatim.
func TestFetchCandleMonth_H4_PlacesTrueTimestamps(t *testing.T) {
	monthStart := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC) // EDT: true boundary 21:00 UTC
	want := datamanager.SlotBoundaries(monthStart, types.H4, 6)

	srv := fakeCandlesServer(t, want)
	defer srv.Close()

	p := provider.New(&oandaclient.Client{BaseURL: srv.URL, Token: "t"})
	month, err := p.FetchCandleMonth(context.Background(), "EUR_USD", types.H4, monthStart)
	require.NoError(t, err)

	for i, wantTime := range want {
		idx := datamanager.SlotIndexForTime(monthStart, types.H4, wantTime)
		require.GreaterOrEqual(t, idx, 0, "slot %d", i)
		require.Less(t, idx, len(month.Candles), "slot %d", i)
		require.False(t, month.Candles[idx].Candle.IsZero(), "slot %d should be filled", i)
		require.Equal(t, types.FromTime(wantTime), month.Candles[idx].Timestamp, "slot %d timestamp", i)
	}
}

// TestFetchCandleMonth_D1_DSTTransition proves D1 candles spanning a DST
// transition (23h day) don't collide into the same slot when downloaded
// directly from OANDA — the same day-walking placement DeriveCanonicalFromRaw
// and local aggregation use.
func TestFetchCandleMonth_D1_DSTTransition(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	all := datamanager.SlotBoundaries(monthStart, types.D1, 31)
	// Just the days straddling the 2026-03-08 US spring-forward transition.
	want := []time.Time{all[5], all[6], all[7]} // Mar 6 (EST), Mar 7 (EST), Mar 8 (EDT)

	srv := fakeCandlesServer(t, want)
	defer srv.Close()

	p := provider.New(&oandaclient.Client{BaseURL: srv.URL, Token: "t"})
	month, err := p.FetchCandleMonth(context.Background(), "EUR_USD", types.D1, monthStart)
	require.NoError(t, err)

	seen := make(map[int]bool)
	for _, wantTime := range want {
		idx := datamanager.SlotIndexForTime(monthStart, types.D1, wantTime)
		require.False(t, seen[idx], "slot %d must not be reused by two different real candles", idx)
		seen[idx] = true
		require.False(t, month.Candles[idx].Candle.IsZero())
		require.Equal(t, types.FromTime(wantTime), month.Candles[idx].Timestamp)
	}
}

// TestFetchCandleMonth_DropsCandleBeforeMonthStart proves a candle
// timestamped before the requested month's first slot is dropped, not
// placed via truncating integer division into slot 0 (the original bug
// this repo's own comment on the old code warned about).
func TestFetchCandleMonth_DropsCandleBeforeMonthStart(t *testing.T) {
	monthStart := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	before := monthStart.Add(-time.Hour)

	srv := fakeCandlesServer(t, []time.Time{before})
	defer srv.Close()

	p := provider.New(&oandaclient.Client{BaseURL: srv.URL, Token: "t"})
	month, err := p.FetchCandleMonth(context.Background(), "EUR_USD", types.H4, monthStart)
	require.NoError(t, err)

	for i, ct := range month.Candles {
		require.True(t, ct.Candle.IsZero(), "slot %d should be empty, the out-of-range candle must be dropped", i)
	}
}
