package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOANDACandlesServer serves a monotonically increasing synthetic candle
// series for any /v3/instruments/{i}/candles request, spaced one hour apart
// starting at the requested "from" query param. Enough candles are returned
// to satisfy any of the review timeframe windows (W/D/H4).
func fakeOANDACandlesServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, err := time.Parse(time.RFC3339Nano, q.Get("from"))
		require.NoError(t, err)

		const n = 200
		type ohlc struct{ O, H, L, C string }
		candles := make([]map[string]any, 0, n)
		price := 1.10000
		for i := range n {
			open := price
			price += 0.00050
			close := price
			bid := ohlc{
				O: fmt.Sprintf("%.5f", open),
				H: fmt.Sprintf("%.5f", close+0.00005),
				L: fmt.Sprintf("%.5f", open-0.00005),
				C: fmt.Sprintf("%.5f", close),
			}
			candles = append(candles, map[string]any{
				"complete": true,
				"time":     from.Add(time.Duration(i) * time.Hour).Format(time.RFC3339Nano),
				"volume":   10,
				"bid":      bid,
				"ask":      bid,
			})
		}

		resp := map[string]any{
			"instrument":  "EUR_USD",
			"granularity": q.Get("granularity"),
			"candles":     candles,
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReviewWatchlist_SingleInstrument(t *testing.T) {
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "EURUSD", resp.Results[0].Instrument)
	assert.NotEmpty(t, resp.Results[0].Bucket)
	assert.False(t, resp.ScannedAt.IsZero())
}

func TestReviewWatchlist_DefaultsToAllInstruments(t *testing.T) {
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{})
	require.NoError(t, err)
	assert.Len(t, resp.Results, len(market.AllInstruments()))
}

func TestReviewWatchlist_SkipsInstrumentOnFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessage":"bad token"}`))
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "bad"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err, "per-instrument failures must not fail the whole run")
	assert.Empty(t, resp.Results)
}

func TestReviewWatchlist_UnknownInstrumentSkipped(t *testing.T) {
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"NOTAPAIR"}})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}
