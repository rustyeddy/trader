package bot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/service"
)

// fakeServer starts a minimal httptest server that serves canned bot responses.
func fakeServer(t *testing.T, statuses []service.BotStatus) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/bots", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statuses)
	})

	mux.HandleFunc("GET /api/v1/bots/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for _, s := range statuses {
			if s.ID == id {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(s)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	mux.HandleFunc("POST /api/v1/bots", func(w http.ResponseWriter, r *http.Request) {
		var cfg service.BotConfig
		json.NewDecoder(r.Body).Decode(&cfg)
		status := service.BotStatus{
			ID:           "bot-test01",
			Instrument:   cfg.Instrument,
			StrategyName: cfg.Strategy.Kind,
			StartedAt:    time.Now().UTC(),
			Status:       "running",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(status)
	})

	mux.HandleFunc("DELETE /api/v1/bots/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for _, s := range statuses {
			if s.ID == id {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "stopped"})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	return httptest.NewServer(mux)
}

func TestBotList_Empty(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	serverURL = srv.URL

	var buf bytes.Buffer
	cmd := botListCmd()
	cmd.SetOut(&buf)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Contains(t, buf.String(), "No bots found")
}

func TestBotList_ShowsBots(t *testing.T) {
	statuses := []service.BotStatus{
		{ID: "bot-aabb", Instrument: "EUR_USD", StrategyName: "donchian-v6", Status: "running", StartedAt: time.Now()},
		{ID: "bot-ccdd", Instrument: "GBP_USD", StrategyName: "ema-cross", Status: "stopped", StartedAt: time.Now()},
	}
	srv := fakeServer(t, statuses)
	defer srv.Close()
	serverURL = srv.URL

	var buf bytes.Buffer
	cmd := botListCmd()
	cmd.SetOut(&buf)

	require.NoError(t, cmd.RunE(cmd, nil))
	out := buf.String()
	assert.Contains(t, out, "bot-aabb")
	assert.Contains(t, out, "EUR_USD")
	assert.Contains(t, out, "donchian-v6")
	assert.Contains(t, out, "bot-ccdd")
}

func TestBotGet_Found(t *testing.T) {
	statuses := []service.BotStatus{
		{ID: "bot-1234", Instrument: "USD_JPY", StrategyName: "scalper", Status: "running", StartedAt: time.Now()},
	}
	srv := fakeServer(t, statuses)
	defer srv.Close()
	serverURL = srv.URL

	var buf bytes.Buffer
	cmd := botGetCmd()
	cmd.SetOut(&buf)

	require.NoError(t, cmd.RunE(cmd, []string{"bot-1234"}))
	out := buf.String()
	assert.Contains(t, out, "bot-1234")
	assert.Contains(t, out, "USD_JPY")
	assert.Contains(t, out, "scalper")
}

func TestBotGet_NotFound(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	serverURL = srv.URL

	cmd := botGetCmd()
	err := cmd.RunE(cmd, []string{"bot-missing"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found"))
}

func TestBotStart(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	serverURL = srv.URL

	var buf bytes.Buffer
	cmd := botStartCmd()
	cmd.SetOut(&buf)
	cmd.Flags().Set("instrument", "EUR_USD")
	cmd.Flags().Set("strategy", "donchian-v6")

	require.NoError(t, cmd.RunE(cmd, nil))
	out := buf.String()
	assert.Contains(t, out, "Bot started")
	assert.Contains(t, out, "bot-test01")
	assert.Contains(t, out, "EUR_USD")
}

func TestBotStop_Found(t *testing.T) {
	statuses := []service.BotStatus{
		{ID: "bot-5678", Status: "running"},
	}
	srv := fakeServer(t, statuses)
	defer srv.Close()
	serverURL = srv.URL

	var buf bytes.Buffer
	cmd := botStopCmd()
	cmd.SetOut(&buf)

	require.NoError(t, cmd.RunE(cmd, []string{"bot-5678"}))
	assert.Contains(t, buf.String(), "bot-5678")
	assert.Contains(t, buf.String(), "stopped")
}

func TestBotStop_NotFound(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	serverURL = srv.URL

	cmd := botStopCmd()
	err := cmd.RunE(cmd, []string{"bot-missing"})
	require.Error(t, err)
}

func TestDefaultServer_EnvVar(t *testing.T) {
	t.Setenv("TRADER_SERVER", "http://myserver:9090")
	assert.Equal(t, "http://myserver:9090", defaultServer())
}

func TestDefaultServer_Fallback(t *testing.T) {
	t.Setenv("TRADER_SERVER", "")
	assert.Equal(t, "http://localhost:8080", defaultServer())
}

func TestBotReport_Args(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	serverURL = srv.URL

	cmd := botReportCmd()
	err := cmd.Args(cmd, nil)
	require.Error(t, err)

	cmd = botReportCmd()
	require.NoError(t, cmd.Flags().Set("all", "true"))
	err = cmd.Args(cmd, []string{"bot-1"})
	require.Error(t, err)

	cmd = botReportCmd()
	require.NoError(t, cmd.Flags().Set("all", "true"))
	err = cmd.Args(cmd, nil)
	require.NoError(t, err)

	cmd = botReportCmd()
	err = cmd.Args(cmd, []string{"bot-1"})
	require.NoError(t, err)
}
