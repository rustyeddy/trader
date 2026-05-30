package oanda

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTradesServer(t *testing.T, body string) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	return &Client{BaseURL: srv.URL, Token: "test"}, srv.Close
}

func TestGetOpenTrades_ParsesOpenTime(t *testing.T) {
	openTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	body := fmt.Sprintf(`{
		"trades": [{
			"id": "42",
			"instrument": "EUR_USD",
			"price": "1.0850",
			"currentUnits": "1000",
			"unrealizedPL": "5.00",
			"openTime": %q
		}]
	}`, openTime.Format(time.RFC3339Nano))

	client, cleanup := newTradesServer(t, body)
	defer cleanup()

	trades, err := client.GetOpenTrades(context.Background(), "acc-1")
	require.NoError(t, err)
	require.Len(t, trades, 1)

	assert.Equal(t, "42", trades[0].ID)
	assert.Equal(t, "EUR_USD", trades[0].Instrument)
	assert.True(t, trades[0].OpenTime.Equal(openTime),
		"expected %v got %v", openTime, trades[0].OpenTime)
}

func TestGetOpenTrades_MissingOpenTimeIsZero(t *testing.T) {
	body := `{"trades":[{"id":"7","instrument":"GBP_USD","price":"1.27","currentUnits":"500","unrealizedPL":"0"}]}`

	client, cleanup := newTradesServer(t, body)
	defer cleanup()

	trades, err := client.GetOpenTrades(context.Background(), "acc-1")
	require.NoError(t, err)
	require.Len(t, trades, 1)
	assert.True(t, trades[0].OpenTime.IsZero())
}

func TestGetOpenTrades_EmptyResponse(t *testing.T) {
	client, cleanup := newTradesServer(t, `{"trades":[]}`)
	defer cleanup()

	trades, err := client.GetOpenTrades(context.Background(), "acc-1")
	require.NoError(t, err)
	assert.Empty(t, trades)
}
