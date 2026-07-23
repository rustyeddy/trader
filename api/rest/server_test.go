package rest

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/rustyeddy/trader/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMinimalServer() *Server {
	return &Server{log: slog.Default()}
}

func TestGetHealth(t *testing.T) {
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/health")
	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestGetHealthV1(t *testing.T) {
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/health")
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetVersion(t *testing.T) {
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/version")
	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Equal(t, config.Version, body["version"])
}
