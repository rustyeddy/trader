package rest

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRestCandleStore seeds a temp store with January 2024 H1 EURUSD candles.
func seedRestCandleStore(t *testing.T) {
	t.Helper()
	candles := make([]market.Candle, 744)
	candles[0] = market.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = market.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	datamanager.SeedCandles(t, "oanda", "EURUSD", types.H1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles)
}

// ── GET /api/v1/candles/{instrument}/stats ────────────────────────────────

func TestHandleDataStats_OK(t *testing.T) {
	seedRestCandleStore(t)

	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EURUSD/stats?timeframe=H1&from=2024-01-01&to=2024-01-31")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "EURUSD", body["instrument"])
	assert.Equal(t, "H1", body["timeframe"])
	analyzers, ok := body["analyzers"].([]any)
	require.True(t, ok)
	assert.Len(t, analyzers, 4)
}

func TestHandleDataStats_DefaultsTimeframeToH1(t *testing.T) {
	seedRestCandleStore(t)

	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EURUSD/stats?from=2024-01-01&to=2024-01-31")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "H1", body["timeframe"])
}

func TestHandleDataStats_MissingFrom(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EURUSD/stats?to=2024-01-31")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleDataStats_MissingTo(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EURUSD/stats?from=2024-01-01")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleDataStats_BadUnits(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EURUSD/stats?from=2024-01-01&to=2024-01-31&units=notanumber")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── GET /api/v1/pip-values ────────────────────────────────────────────────

func TestHandlePipValues_DefaultsMajorPairs(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/pip-values")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	rows, ok := body["rows"].([]any)
	require.True(t, ok)
	assert.Len(t, rows, 7)
}

func TestHandlePipValues_ScopesToRequestedInstruments(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/pip-values?instruments=EURUSD,USDJPY")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	rows, ok := body["rows"].([]any)
	require.True(t, ok)
	assert.Len(t, rows, 2)
}

func TestHandlePipValues_BadUnits(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/pip-values?units=abc")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── GET /api/v1/position ──────────────────────────────────────────────────

func TestHandlePosition_ReturnsTable(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?instrument=EURUSD&price=1.08")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	rows, ok := body["rows"].([]any)
	require.True(t, ok)
	assert.Len(t, rows, 3)
}

func TestHandlePosition_MissingInstrument(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?price=1.08")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandlePosition_BadPrice(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?instrument=EURUSD&price=abc")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandlePosition_BadUnits(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?instrument=EURUSD&price=1.08&units=notanumber")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandlePosition_BadNotional(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?instrument=EURUSD&price=1.08&notional=abc")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandlePosition_BadPips(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/position?instrument=EURUSD&price=1.08&pips=abc")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── GET /api/v1/candles/validate ─────────────────────────────────────────

func TestHandleValidateCandles_MissingInstruments(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/validate?from=2024-01&to=2024-01")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleValidateCandles_MissingFrom(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/validate?instruments=EURUSD&to=2024-01")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleValidateCandles_BadFromFormat(t *testing.T) {
	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/validate?instruments=EURUSD&from=notadate&to=2024-01")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleValidateCandles_OK(t *testing.T) {
	seedRestCandleStore(t)

	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/validate?instruments=EURUSD&from=2024-01&to=2024-01")
	require.Equal(t, http.StatusOK, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	_, hasSource := body["source"]
	assert.True(t, hasSource)
	_, hasMonths := body["months_scanned"]
	assert.True(t, hasMonths)
}
