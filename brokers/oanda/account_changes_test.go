package oanda

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func changesServer(t *testing.T, path, body string) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, path, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	return &Client{BaseURL: srv.URL, Token: "tok"}, srv.Close
}

func TestGetAccountDetails_ParsesFullSnapshot(t *testing.T) {
	openTime := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	body := `{
		"account": {
			"id": "001-002-123",
			"alias": "main",
			"currency": "USD",
			"balance": "10000.0000",
			"NAV": "10150.0000",
			"unrealizedPL": "150.0000",
			"marginUsed": "200.0000",
			"marginAvailable": "9950.0000",
			"lastTransactionID": "42",
			"trades": [{
				"id": "7",
				"instrument": "EUR_USD",
				"price": "1.0850",
				"currentUnits": "1000",
				"unrealizedPL": "150.0000",
				"openTime": "2024-06-01T12:00:00.000000Z"
			}]
		},
		"lastTransactionID": "42"
	}`

	client, cleanup := changesServer(t, "/v3/accounts/001-002-123", body)
	defer cleanup()

	details, err := client.GetAccountDetails(context.Background(), "001-002-123")
	require.NoError(t, err)
	require.NotNil(t, details)

	assert.Equal(t, "001-002-123", details.ID)
	assert.Equal(t, "main", details.Alias)
	assert.Equal(t, "USD", details.Currency)
	assert.InDelta(t, 10000.0, details.Balance, 0.001)
	assert.InDelta(t, 10150.0, details.NAV, 0.001)
	assert.InDelta(t, 150.0, details.UnrealizedPL, 0.001)
	assert.InDelta(t, 200.0, details.MarginUsed, 0.001)
	assert.InDelta(t, 9950.0, details.MarginAvail, 0.001)
	assert.Equal(t, int64(42), details.LastTransactionID)

	require.Len(t, details.OpenTrades, 1)
	tr := details.OpenTrades[0]
	assert.Equal(t, "7", tr.ID)
	assert.Equal(t, "EUR_USD", tr.Instrument)
	assert.InDelta(t, 1.0850, tr.EntryPrice, 0.00001)
	assert.Equal(t, int64(1000), tr.Units)
	assert.InDelta(t, 150.0, tr.UnrealizedPL, 0.001)
	assert.True(t, tr.OpenTime.Equal(openTime), "openTime mismatch")
}

func TestGetAccountDetails_EmptyTrades(t *testing.T) {
	body := `{
		"account": {
			"id": "acc-1",
			"balance": "5000.00",
			"NAV": "5000.00",
			"unrealizedPL": "0",
			"marginUsed": "0",
			"marginAvailable": "5000.00",
			"lastTransactionID": "1",
			"trades": []
		},
		"lastTransactionID": "1"
	}`
	client, cleanup := changesServer(t, "/v3/accounts/acc-1", body)
	defer cleanup()

	details, err := client.GetAccountDetails(context.Background(), "acc-1")
	require.NoError(t, err)
	assert.Empty(t, details.OpenTrades)
	assert.Equal(t, int64(1), details.LastTransactionID)
}

func TestGetAccountChanges_AppliesTradesAndState(t *testing.T) {
	body := `{
		"changes": {
			"tradesOpened": [{
				"id": "11",
				"instrument": "GBP_USD",
				"price": "1.2700",
				"currentUnits": "500",
				"unrealizedPL": "10.00",
				"openTime": "2024-06-01T14:00:00.000000Z"
			}],
			"tradesClosed": [{"id": "7"}],
			"tradesReduced": [{"id": "8", "currentUnits": "250"}],
			"transactions": [
				{"type": "ORDER_FILL", "accountBalance": "10050.0000"}
			]
		},
		"state": {
			"NAV": "10060.0000",
			"unrealizedPL": "60.0000",
			"marginUsed": "180.0000",
			"marginAvailable": "9880.0000",
			"trades": [
				{"id": "11", "unrealizedPL": "10.00"},
				{"id": "8",  "unrealizedPL": "50.00"}
			]
		},
		"lastTransactionID": "55"
	}`

	client, cleanup := changesServer(t, "/v3/accounts/acc-1/changes", body)
	defer cleanup()

	result, err := client.GetAccountChanges(context.Background(), "acc-1", 42)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.TradesOpened, 1)
	assert.Equal(t, "11", result.TradesOpened[0].ID)
	assert.Equal(t, "GBP_USD", result.TradesOpened[0].Instrument)

	require.Len(t, result.TradesClosed, 1)
	assert.Equal(t, "7", result.TradesClosed[0])

	require.Len(t, result.TradesReduced, 1)
	assert.Equal(t, int64(250), result.TradesReduced["8"])

	assert.InDelta(t, 10050.0, result.BalanceAfterFill, 0.001)
	assert.InDelta(t, 10060.0, result.NAV, 0.001)
	assert.InDelta(t, 60.0, result.UnrealizedPL, 0.001)
	assert.InDelta(t, 180.0, result.MarginUsed, 0.001)
	assert.InDelta(t, 9880.0, result.MarginAvail, 0.001)

	assert.InDelta(t, 10.0, result.TradeState["11"], 0.001)
	assert.InDelta(t, 50.0, result.TradeState["8"], 0.001)

	assert.Equal(t, int64(55), result.LastTransactionID)
}

func TestGetAccountChanges_NoChanges(t *testing.T) {
	body := `{
		"changes": {},
		"state": {
			"NAV": "10000.0000",
			"unrealizedPL": "0",
			"marginUsed": "0",
			"marginAvailable": "10000.0000"
		},
		"lastTransactionID": "42"
	}`

	client, cleanup := changesServer(t, "/v3/accounts/acc-1/changes", body)
	defer cleanup()

	result, err := client.GetAccountChanges(context.Background(), "acc-1", 42)
	require.NoError(t, err)
	assert.Empty(t, result.TradesOpened)
	assert.Empty(t, result.TradesClosed)
	assert.Equal(t, int64(42), result.LastTransactionID)
}
