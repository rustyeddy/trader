//go:build blackbox

package rest_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/api/rest"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBlackboxServer starts a real httptest.Server backed by a Server with
// no OANDA client (backtest-only mode) and a temp reports directory.
func newBlackboxServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	svc := &service.Service{Log: slog.Default()}
	srv := rest.New(svc, ":0")
	dir := t.TempDir()
	srv.WithReportsDir(dir)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func get(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, v))
}

func writeReport(t *testing.T, dir string, s trader.BacktestReportSummary) {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, s.Name+".json"), append(b, '\n'), 0o644))
}

// ── Health ────────────────────────────────────────────────────────────────────

func TestBlackbox_Health(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	for _, path := range []string{"/health", "/api/v1/health"} {
		resp := get(t, ts, path)
		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json", path)

		var body map[string]string
		decodeJSON(t, resp, &body)
		assert.Equal(t, "ok", body["status"], path)
	}
}

// ── CORS ──────────────────────────────────────────────────────────────────────

func TestBlackbox_CORS_ResponseHeaders(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp := get(t, ts, "/health")
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "GET")
}

func TestBlackbox_CORS_Preflight(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/backtests", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

// ── OANDA-gated endpoints return 503 without a token ─────────────────────────

func TestBlackbox_NoOANDA_Account503(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp := get(t, ts, "/api/v1/accounts/acc/account")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]string
	decodeJSON(t, resp, &body)
	assert.Contains(t, body["error"], "OANDA")
}

func TestBlackbox_NoOANDA_Trades503(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, ts, "/api/v1/accounts/acc/trades").StatusCode)
}

func TestBlackbox_NoOANDA_Transactions503(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, ts, "/api/v1/accounts/acc/transactions").StatusCode)
}

func TestBlackbox_NoOANDA_StreamAccount503(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, ts, "/api/v1/accounts/acc/stream/account").StatusCode)
}

func TestBlackbox_NoOANDA_StreamEvents503(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, ts, "/api/v1/accounts/acc/stream/events").StatusCode)
}

// ── Stream backtest placeholder ────────────────────────────────────────────────

func TestBlackbox_StreamBacktest_NotImplemented(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusNotImplemented, get(t, ts, "/api/v1/stream/backtest/any-id").StatusCode)
}

// ── Backtest list ─────────────────────────────────────────────────────────────

func TestBlackbox_ListBacktests_Empty(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp := get(t, ts, "/api/v1/backtests")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, float64(0), body["count"])
}

func TestBlackbox_ListBacktests_WithReports(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	writeReport(t, dir, trader.BacktestReportSummary{Name: "run-a", Instrument: "EURUSD"})
	writeReport(t, dir, trader.BacktestReportSummary{Name: "run-b", Instrument: "GBPUSD"})

	resp := get(t, ts, "/api/v1/backtests")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Count int `json:"count"`
	}
	decodeJSON(t, resp, &body)
	assert.Equal(t, 2, body.Count)
}

func TestBlackbox_ListBacktests_InstrumentFilter(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	writeReport(t, dir, trader.BacktestReportSummary{Name: "eur", Instrument: "EURUSD"})
	writeReport(t, dir, trader.BacktestReportSummary{Name: "gbp", Instrument: "GBPUSD"})

	resp := get(t, ts, "/api/v1/backtests?instrument=EUR")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Count int `json:"count"`
	}
	decodeJSON(t, resp, &body)
	assert.Equal(t, 1, body.Count)
}

func TestBlackbox_ListBacktests_StrategyFilter(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	writeReport(t, dir, trader.BacktestReportSummary{Name: "e1", Strategy: "ema-cross"})
	writeReport(t, dir, trader.BacktestReportSummary{Name: "r1", Strategy: "rsi-mean"})

	resp := get(t, ts, "/api/v1/backtests?strategy=ema")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Count int `json:"count"`
	}
	decodeJSON(t, resp, &body)
	assert.Equal(t, 1, body.Count)
}

func TestBlackbox_ListBacktests_TradeDetailsStripped(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	writeReport(t, dir, trader.BacktestReportSummary{
		Name:         "with-trades",
		TradeDetails: []trader.BacktestReportTrade{{ID: "t1"}},
	})

	resp := get(t, ts, "/api/v1/backtests")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Summaries []trader.BacktestReportSummary `json:"summaries"`
	}
	decodeJSON(t, resp, &body)
	require.Len(t, body.Summaries, 1)
	assert.Nil(t, body.Summaries[0].TradeDetails, "list must omit trade_details")
}

// ── Backtest get ──────────────────────────────────────────────────────────────

func TestBlackbox_GetBacktest_Found(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	writeReport(t, dir, trader.BacktestReportSummary{
		Name:         "detail-run",
		Trades:       12,
		TradeDetails: []trader.BacktestReportTrade{{ID: "t1", Side: "long"}},
	})

	resp := get(t, ts, "/api/v1/backtests/detail-run")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got trader.BacktestReportSummary
	decodeJSON(t, resp, &got)
	assert.Equal(t, "detail-run", got.Name)
	assert.Equal(t, 12, got.Trades)
	require.Len(t, got.TradeDetails, 1, "get must include trade_details")
}

func TestBlackbox_GetBacktest_NotFound(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusNotFound, get(t, ts, "/api/v1/backtests/ghost").StatusCode)
}

// ── Backtest org download ─────────────────────────────────────────────────────

func TestBlackbox_GetBacktestOrg_Found(t *testing.T) {
	ts, dir := newBlackboxServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-x.org"), []byte("* Org content\n"), 0o644))

	resp := get(t, ts, "/api/v1/backtests/run-x/org")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "run-x.org")
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}

func TestBlackbox_GetBacktestOrg_NotFound(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	assert.Equal(t, http.StatusNotFound, get(t, ts, "/api/v1/backtests/missing/org").StatusCode)
}

// ── Backtest run — error paths ────────────────────────────────────────────────

func TestBlackbox_RunBacktest_BadJSON(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp, err := http.Post(ts.URL+"/api/v1/backtests/run",
		"application/json", strings.NewReader("not-json"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestBlackbox_RunBacktest_EmptyConfigPaths(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp, err := http.Post(ts.URL+"/api/v1/backtests/run",
		"application/json", strings.NewReader(`{"config_paths":[]}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestBlackbox_RunBacktest_NonExistentConfig(t *testing.T) {
	ts, _ := newBlackboxServer(t)
	resp, err := http.Post(ts.URL+"/api/v1/backtests/run",
		"application/json", strings.NewReader(`{"config_paths":["/no/such/config.yml"]}`))
	require.NoError(t, err)
	resp.Body.Close()
	// config load fails → service returns error → 500
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
