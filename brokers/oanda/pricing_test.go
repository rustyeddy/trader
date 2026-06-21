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

func pricingServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func pricingBody(prices ...map[string]any) map[string]any {
	return map[string]any{"prices": prices}
}

func priceEntry(instrument, bid, ask string) map[string]any {
	return map[string]any{
		"instrument": instrument,
		"bids":       []any{map[string]any{"price": bid}},
		"asks":       []any{map[string]any{"price": ask}},
	}
}

func TestGetPricing_SingleInstrument(t *testing.T) {
	srv := pricingServer(t, 200, pricingBody(priceEntry("EUR_USD", "1.08490", "1.08510")))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	prices, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.NoError(t, err)
	require.Len(t, prices, 1)
	assert.Equal(t, "EUR_USD", prices[0].Instrument)
	assert.InDelta(t, 1.08490, prices[0].Bid, 1e-9)
	assert.InDelta(t, 1.08510, prices[0].Ask, 1e-9)
	assert.InDelta(t, 1.08500, prices[0].Mid, 1e-9)
}

func TestGetPricing_MultipleInstruments(t *testing.T) {
	srv := pricingServer(t, 200, pricingBody(
		priceEntry("EUR_USD", "1.08490", "1.08510"),
		priceEntry("USD_JPY", "149.990", "150.010"),
	))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	prices, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD", "USD_JPY")
	require.NoError(t, err)
	assert.Len(t, prices, 2)
}

func TestGetPricing_NoInstrumentsReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: "tok"}
	_, err := c.GetPricing(context.Background(), "ACC1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one instrument")
}

func TestGetPricing_EntryWithNoBidsSkipped(t *testing.T) {
	body := map[string]any{
		"prices": []any{
			map[string]any{
				"instrument": "EUR_USD",
				"bids":       []any{},
				"asks":       []any{map[string]any{"price": "1.08510"}},
			},
		},
	}
	srv := pricingServer(t, 200, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	prices, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.NoError(t, err)
	assert.Empty(t, prices)
}

func TestGetPricing_BadBidReturnsError(t *testing.T) {
	body := map[string]any{
		"prices": []any{
			map[string]any{
				"instrument": "EUR_USD",
				"bids":       []any{map[string]any{"price": "bad"}},
				"asks":       []any{map[string]any{"price": "1.08510"}},
			},
		},
	}
	srv := pricingServer(t, 200, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse bid")
}

func TestGetPricing_BadAskReturnsError(t *testing.T) {
	body := map[string]any{
		"prices": []any{
			map[string]any{
				"instrument": "EUR_USD",
				"bids":       []any{map[string]any{"price": "1.08490"}},
				"asks":       []any{map[string]any{"price": "bad"}},
			},
		},
	}
	srv := pricingServer(t, 200, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse ask")
}

func TestGetPricing_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse pricing response")
}

func TestGetPricing_HTTPErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.GetPricing(context.Background(), "ACC1", "EUR_USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
