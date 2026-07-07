package oanda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accountSummaryServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestGetAccountSummary_ParsesAllFields(t *testing.T) {
	srv := accountSummaryServer(t, 200, map[string]any{
		"account": map[string]any{
			"id":              "001-001-123",
			"alias":           "primary",
			"currency":        "USD",
			"balance":         "10000.50",
			"NAV":             "10250.75",
			"unrealizedPL":    "250.25",
			"marginUsed":      "500.00",
			"marginAvailable": "9750.75",
		},
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	s, err := c.GetAccountSummary(context.Background(), "ACC1")
	require.NoError(t, err)

	assert.Equal(t, "001-001-123", s.ID)
	assert.Equal(t, "primary", s.Alias)
	assert.Equal(t, "USD", s.Currency)
	assert.InDelta(t, 10000.50, s.Balance, 1e-9)
	assert.InDelta(t, 10250.75, s.NAV, 1e-9)
	assert.InDelta(t, 250.25, s.UnrealizedPL, 1e-9)
	assert.InDelta(t, 500.00, s.MarginUsed, 1e-9)
	assert.InDelta(t, 9750.75, s.MarginAvail, 1e-9)
}

func TestGetAccountSummary_EmptyNumericFieldsTreatedAsZero(t *testing.T) {
	// OANDA may omit numeric fields for demo accounts; empty string → 0.
	srv := accountSummaryServer(t, 200, map[string]any{
		"account": map[string]any{
			"id":       "ACC1",
			"currency": "USD",
		},
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	s, err := c.GetAccountSummary(context.Background(), "ACC1")
	require.NoError(t, err)
	assert.Equal(t, 0.0, s.Balance)
	assert.Equal(t, 0.0, s.NAV)
}

func TestGetAccountSummary_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetAccountSummary(context.Background(), "ACC1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse account summary")
}

func TestGetAccountSummary_BadBalanceFieldReturnsError(t *testing.T) {
	srv := accountSummaryServer(t, 200, map[string]any{
		"account": map[string]any{
			"id":      "ACC1",
			"balance": "not-a-number",
		},
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetAccountSummary(context.Background(), "ACC1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "balance")
}

func TestGetAccountSummary_HTTPErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetAccountSummary(context.Background(), "ACC1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
