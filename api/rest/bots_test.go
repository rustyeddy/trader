package rest

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	botsvc "github.com/rustyeddy/trader/service/bots"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/rustyeddy/trader/strategies/pulse"
)

// newBotsTestServer builds a Server backed by a Service with a real (but
// sandboxed) OANDA client. We point it at a fake HTTP server so no real
// network calls are made.
func newBotsTestServer(t *testing.T) *Server {
	t.Helper()
	// Minimal stub that won't be called during these unit tests.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stub.Close)
	client, err := oanda.NewClient("practice", "test-token")
	if err != nil {
		t.Fatalf("oanda.NewClient: %v", err)
	}
	// Override the base URL so any accidental call hits the stub.
	client.BaseURL = stub.URL
	return New(client, slog.Default(), "test-account", nil, ":0")
}

func TestHandleListBots_Empty(t *testing.T) {
	srv := newBotsTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/test-account/bots", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)

	var result []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Empty(t, result)
}

func TestHandleGetBot_NotFound(t *testing.T) {
	srv := newBotsTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/bots/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleStopBot_NotFound(t *testing.T) {
	srv := newBotsTestServer(t)
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/bots/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleStartBot_NoOANDA(t *testing.T) {
	srv := New(nil, slog.Default(), "", nil, ":0")
	body, _ := json.Marshal(botsvc.BotConfig{
		Instrument: "EUR_USD",
		Strategy:   botsvc.StrategyConfig{Kind: "pulse"},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/test-account/bots", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStartBot_BadJSON(t *testing.T) {
	srv := newBotsTestServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/test-account/bots", bytes.NewReader([]byte("notjson")))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartBot_MissingInstrument(t *testing.T) {
	srv := newBotsTestServer(t)
	body, _ := json.Marshal(botsvc.BotConfig{
		Strategy: botsvc.StrategyConfig{Kind: "pulse"},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/test-account/bots", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "instrument")
}

func TestHandleStartBot_UnknownStrategy(t *testing.T) {
	srv := newBotsTestServer(t)
	body, _ := json.Marshal(botsvc.BotConfig{
		Instrument: "EUR_USD",
		Strategy:   botsvc.StrategyConfig{Kind: "bogus"},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/test-account/bots", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartBot_CreatesBot(t *testing.T) {
	srv := newBotsTestServer(t)
	body, _ := json.Marshal(botsvc.BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "24h", // long interval — won't actually tick during test
		Strategy: botsvc.StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/test-account/bots", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusCreated, w.Code)

	var status botsvc.BotStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	assert.Equal(t, "running", status.Status)
	assert.Equal(t, "EUR_USD", status.Instrument)
	assert.NotEmpty(t, status.ID)

	// GET /api/v1/bots should list it.
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/test-account/bots", nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, r2)
	require.Equal(t, http.StatusOK, w2.Code)
	var bots []botsvc.BotStatus
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &bots))
	require.Len(t, bots, 1)
	assert.Equal(t, status.ID, bots[0].ID)

	// GET /api/v1/bots/{id} should return it.
	r3 := httptest.NewRequest(http.MethodGet, "/api/v1/bots/"+status.ID, nil)
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, r3)
	require.Equal(t, http.StatusOK, w3.Code)

	// DELETE /api/v1/bots/{id} should stop it.
	r4 := httptest.NewRequest(http.MethodDelete, "/api/v1/bots/"+status.ID, nil)
	w4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w4, r4)
	require.Equal(t, http.StatusOK, w4.Code)
	var stopResp map[string]string
	require.NoError(t, json.Unmarshal(w4.Body.Bytes(), &stopResp))
	assert.Equal(t, "stopped", stopResp["status"])
}
