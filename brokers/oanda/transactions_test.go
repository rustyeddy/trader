package oanda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── parseTransaction unit tests (no HTTP) ────────────────────────────────────

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestParseTransaction_MinimalFields(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":        "42",
		"batchID":   "41",
		"accountID": "ACC1",
		"type":      "MARKET_ORDER",
	})
	tx, err := parseTransaction(raw)
	require.NoError(t, err)
	assert.Equal(t, "42", tx.ID)
	assert.Equal(t, "41", tx.BatchID)
	assert.Equal(t, "ACC1", tx.AccountID)
	assert.Equal(t, "MARKET_ORDER", tx.Type)
	assert.True(t, tx.Time.IsZero())
}

func TestParseTransaction_OrderFillWithAllFields(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":             "100",
		"type":           "ORDER_FILL",
		"time":           "2024-06-15T10:30:00.000000000Z",
		"instrument":     "EUR_USD",
		"units":          "10000",
		"price":          "1.08500",
		"pl":             "25.00",
		"accountBalance": "10025.00",
		"orderID":        "99",
		"reason":         "MARKET_ORDER",
		"tradeOpened":    map[string]any{"tradeID": "55"},
	})
	tx, err := parseTransaction(raw)
	require.NoError(t, err)
	assert.Equal(t, "ORDER_FILL", tx.Type)
	assert.Equal(t, "EUR_USD", tx.Instrument)
	assert.Equal(t, int64(10000), tx.Units)
	assert.InDelta(t, 1.085, tx.Price, 1e-9)
	assert.InDelta(t, 25.0, tx.PL, 1e-9)
	assert.InDelta(t, 10025.0, tx.AccountBalance, 1e-9)
	assert.Equal(t, "99", tx.OrderID)
	assert.Equal(t, "55", tx.TradeID)
	assert.Equal(t, 2024, tx.Time.Year())
	assert.Equal(t, time.June, tx.Time.Month())
}

func TestParseTransaction_TradesClosed(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":   "200",
		"type": "ORDER_FILL",
		"tradesClosed": []any{
			map[string]any{
				"tradeID":    "55",
				"units":      "-10000",
				"price":      "1.09000",
				"realizedPL": "50.00",
			},
		},
	})
	tx, err := parseTransaction(raw)
	require.NoError(t, err)
	require.Len(t, tx.TradesClosed, 1)
	assert.Equal(t, "55", tx.TradesClosed[0].TradeID)
	assert.Equal(t, int64(-10000), tx.TradesClosed[0].Units)
	assert.InDelta(t, 1.09, tx.TradesClosed[0].Price, 1e-9)
	assert.InDelta(t, 50.0, tx.TradesClosed[0].RealizedPL, 1e-9)
}

func TestParseTransaction_BadTimeReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":   "1",
		"time": "not-a-time",
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_BadUnitsReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":    "1",
		"units": "abc",
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_BadPriceReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":    "1",
		"price": "bad",
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_BadPLReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id": "1",
		"pl": "bad",
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_BadAccountBalanceReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":             "1",
		"accountBalance": "bad",
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_BadClosedTradeUnitsReturnsError(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"id":   "1",
		"type": "ORDER_FILL",
		"tradesClosed": []any{
			map[string]any{"tradeID": "5", "units": "bad", "price": "1.0", "realizedPL": "0"},
		},
	})
	_, err := parseTransaction(raw)
	require.Error(t, err)
}

func TestParseTransaction_MalformedJSONReturnsError(t *testing.T) {
	_, err := parseTransaction(json.RawMessage(`{bad json`))
	require.Error(t, err)
}

// ── GetTransactions HTTP integration tests ───────────────────────────────────

func transactionServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestGetTransactions_EmptyList(t *testing.T) {
	srv := transactionServer(t, 200, map[string]any{
		"transactions":      []any{},
		"lastTransactionID": "42",
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	txs, lastID, err := c.GetTransactions(context.Background(), "ACC1", 0)
	require.NoError(t, err)
	assert.Empty(t, txs)
	assert.Equal(t, int64(42), lastID)
}

func TestGetTransactions_OneTransaction(t *testing.T) {
	tx := map[string]any{
		"id":         "10",
		"batchID":    "10",
		"accountID":  "ACC1",
		"type":       "MARKET_ORDER",
		"instrument": "EUR_USD",
		"units":      "5000",
	}
	srv := transactionServer(t, 200, map[string]any{
		"transactions":      []any{tx},
		"lastTransactionID": "10",
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	txs, lastID, err := c.GetTransactions(context.Background(), "ACC1", 0)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Equal(t, "10", txs[0].ID)
	assert.Equal(t, int64(5000), txs[0].Units)
	assert.Equal(t, int64(10), lastID)
}

func TestGetTransactions_MissingTokenReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: ""}
	_, _, err := c.GetTransactions(context.Background(), "ACC1", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing token")
}

func TestGetTransactions_MissingAccountIDReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: "tok"}
	_, _, err := c.GetTransactions(context.Background(), "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing account ID")
}

func TestGetTransactions_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, _, err := c.GetTransactions(context.Background(), "ACC1", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse transactions")
}

func TestGetTransactions_BadLastTransactionIDReturnsError(t *testing.T) {
	srv := transactionServer(t, 200, map[string]any{
		"transactions":      []any{},
		"lastTransactionID": "not-an-int",
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, _, err := c.GetTransactions(context.Background(), "ACC1", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lastTransactionID")
}

func TestGetTransactions_HTTPErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, _, err := c.GetTransactions(context.Background(), "ACC1", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
