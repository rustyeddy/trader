package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapTempStore points the global candle store at a fresh temp directory for
// the duration of the test, so review's local-cache reads/writes never touch
// the live /srv/trading/data store.
func swapTempStore(t *testing.T) {
	t.Helper()
	datamanager.UseTempDataDir(t)
}

// fakeOANDACandlesServer serves a monotonically increasing synthetic candle
// series large enough to satisfy any review timeframe window, spaced at the
// requested granularity's real duration so the local-store grid the review
// path writes through lands one candle per day/4h/week slot.
func fakeOANDACandlesServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, err := time.Parse(time.RFC3339Nano, q.Get("from"))
		require.NoError(t, err)

		const n = 300

		// D/H4 are daily-alignment grids (17:00 America/New_York,
		// DST-aware) — OANDA's real candles land there, not on a naive
		// fixed-duration stride from "from". Use the same boundary walk
		// production reads/writes through (datamanager.SlotBoundaries) so
		// this fake server's output matches what a real OANDA response
		// would place candles at.
		var times []time.Time
		switch q.Get("granularity") {
		case "D":
			times = datamanager.SlotBoundaries(from, types.D1, n)
		case "H4":
			times = datamanager.SlotBoundaries(from, types.H4, n)
		default:
			step := time.Hour
			if q.Get("granularity") == "W" {
				step = 7 * 24 * time.Hour
			}
			times = make([]time.Time, n)
			for i := range times {
				times[i] = from.Add(time.Duration(i) * step)
			}
		}

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
				"time":     times[i].Format(time.RFC3339Nano),
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

func TestHandleReview_ReturnsResults(t *testing.T) {
	swapTempStore(t)
	oandaSrv := fakeOANDACandlesServer(t)
	defer oandaSrv.Close()

	svc := &service.Service{
		OANDA: &oanda.Client{BaseURL: oandaSrv.URL, Token: "t"},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := New(svc, "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/review?instruments=EURUSD", nil)
	srv.Handler().ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var got service.ReviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Results, 1)
	assert.Equal(t, "EURUSD", got.Results[0].Instrument)
}

func TestHandleReview_DefaultsToAllInstruments(t *testing.T) {
	swapTempStore(t)
	oandaSrv := fakeOANDACandlesServer(t)
	defer oandaSrv.Close()

	svc := &service.Service{
		OANDA: &oanda.Client{BaseURL: oandaSrv.URL, Token: "t"},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := New(svc, "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/review", nil)
	srv.Handler().ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var got service.ReviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got.Results, len(market.AllInstruments()))
}

func TestHandleReview_OANDAErrorYieldsEmptyResultsNot502(t *testing.T) {
	swapTempStore(t)
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer badSrv.Close()

	// Per-instrument fetch failures are skipped inside ReviewWatchlist, so a
	// bad OANDA client still yields 200 with an empty result set, not 502.
	svc := &service.Service{
		OANDA: &oanda.Client{BaseURL: badSrv.URL, Token: "bad"},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := New(svc, "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/review?instruments=EURUSD", nil)
	srv.Handler().ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var got service.ReviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Empty(t, got.Results)
}
