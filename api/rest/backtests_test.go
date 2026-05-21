package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer returns a Server wired to a temporary reports directory
// pre-populated with the given summaries written as JSON files.
func newTestServer(t *testing.T, summaries []trader.BacktestReportSummary) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	for _, s := range summaries {
		b, err := json.MarshalIndent(s, "", "  ")
		require.NoError(t, err)
		path := filepath.Join(dir, s.Name+".json")
		require.NoError(t, os.WriteFile(path, append(b, '\n'), 0o644))
	}
	srv := &Server{reportsDir: dir}
	return srv, dir
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ── handleListBacktests ───────────────────────────────────────────────────────

func TestHandleListBacktests_Empty(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests")

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, float64(0), body["count"])
	assert.Nil(t, body["summaries"])
}

func TestHandleListBacktests_ReturnsSummaries(t *testing.T) {
	summaries := []trader.BacktestReportSummary{
		{Name: "run-a", Strategy: "ema", Instrument: "EURUSD", Trades: 10, WinRate: 60},
		{Name: "run-b", Strategy: "rsi", Instrument: "GBPUSD", Trades: 5, WinRate: 40},
	}
	srv, _ := newTestServer(t, summaries)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests")

	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Count     int                            `json:"count"`
		Summaries []trader.BacktestReportSummary `json:"summaries"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, 2, body.Count)
	assert.Len(t, body.Summaries, 2)
}

func TestHandleListBacktests_TradeDetailsOmitted(t *testing.T) {
	s := trader.BacktestReportSummary{
		Name: "with-trades",
		TradeDetails: []trader.BacktestReportTrade{
			{ID: "t1", Side: "long", PNL: 100},
		},
	}
	srv, _ := newTestServer(t, []trader.BacktestReportSummary{s})
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests")

	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Summaries []trader.BacktestReportSummary `json:"summaries"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Summaries, 1)
	assert.Nil(t, body.Summaries[0].TradeDetails, "list view must omit trade_details")
}

func TestHandleListBacktests_FilterByInstrument(t *testing.T) {
	summaries := []trader.BacktestReportSummary{
		{Name: "eur", Instrument: "EURUSD"},
		{Name: "gbp", Instrument: "GBPUSD"},
	}
	srv, _ := newTestServer(t, summaries)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests?instrument=EUR")

	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Count     int                            `json:"count"`
		Summaries []trader.BacktestReportSummary `json:"summaries"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, 1, body.Count)
	assert.Equal(t, "EURUSD", body.Summaries[0].Instrument)
}

func TestHandleListBacktests_FilterByStrategy(t *testing.T) {
	summaries := []trader.BacktestReportSummary{
		{Name: "e1", Strategy: "ema-cross"},
		{Name: "r1", Strategy: "rsi-mean"},
	}
	srv, _ := newTestServer(t, summaries)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests?strategy=ema")

	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, 1, body.Count)
}

// ── handleGetBacktest ─────────────────────────────────────────────────────────

func TestHandleGetBacktest_Found(t *testing.T) {
	s := trader.BacktestReportSummary{
		Name:     "run-detail",
		Strategy: "ema",
		Trades:   7,
		TradeDetails: []trader.BacktestReportTrade{
			{ID: "t1", Side: "long", PNL: 50},
		},
	}
	srv, _ := newTestServer(t, []trader.BacktestReportSummary{s})
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/run-detail")

	require.Equal(t, http.StatusOK, rr.Code)
	var got trader.BacktestReportSummary
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "run-detail", got.Name)
	assert.Equal(t, 7, got.Trades)
	require.Len(t, got.TradeDetails, 1, "detail view must include trade_details")
	assert.Equal(t, "t1", got.TradeDetails[0].ID)
}

func TestHandleGetBacktest_WithJsonSuffix(t *testing.T) {
	s := trader.BacktestReportSummary{Name: "run-x", Strategy: "rsi"}
	srv, _ := newTestServer(t, []trader.BacktestReportSummary{s})

	// Requesting with explicit .json suffix should also work.
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/run-x.json")
	require.Equal(t, http.StatusOK, rr.Code)
	var got trader.BacktestReportSummary
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "run-x", got.Name)
}

func TestHandleGetBacktest_NotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/does-not-exist")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandleGetBacktest_PathTraversalRejected(t *testing.T) {
	// Go's mux normalises "../../etc/passwd" out of the URL before the handler
	// runs (returns 301), and filepath.Base in the handler strips any remaining
	// separators. Either way the response must never be 200.
	srv, _ := newTestServer(t, nil)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/../../etc/passwd")
	assert.NotEqual(t, http.StatusOK, rr.Code)
}

// ── handleGetBacktestOrg ──────────────────────────────────────────────────────

func TestHandleGetBacktestOrg_Found(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run-y.org"), []byte("* Org report\n"), 0o644))
	srv := &Server{reportsDir: dir}

	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/run-y/org")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "run-y.org")
	assert.Equal(t, "* Org report\n", rr.Body.String())
}

func TestHandleGetBacktestOrg_NotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	rr := do(t, srv.Handler(), "GET", "/api/v1/backtests/missing/org")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── loadSummary ────────────────────────────────────────────────────────────────

func TestLoadSummary_BackfillsNameFromFilename(t *testing.T) {
	// JSON with no "name" field — loadSummary should derive it from the filename.
	dir := t.TempDir()
	path := filepath.Join(dir, "inferred-name.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"strategy":"ema"}`), 0o644))

	s, err := loadSummary(path)
	require.NoError(t, err)
	assert.Equal(t, "inferred-name", s.Name)
}

func TestLoadSummary_PreservesExplicitName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	b, _ := json.Marshal(trader.BacktestReportSummary{Name: "explicit"})
	require.NoError(t, os.WriteFile(path, b, 0o644))

	s, err := loadSummary(path)
	require.NoError(t, err)
	assert.Equal(t, "explicit", s.Name)
}

func TestLoadSummary_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o644))

	_, err := loadSummary(path)
	assert.Error(t, err)
}

func TestLoadSummary_MissingFile(t *testing.T) {
	_, err := loadSummary("/nonexistent/path/file.json")
	assert.Error(t, err)
}
