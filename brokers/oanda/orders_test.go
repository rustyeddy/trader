package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// orderFillBody builds the minimal OANDA order-fill JSON for tests.
// tradeOpened, tradesClosed, tradeReduced control which netting variant is set.
func orderFillBody(t *testing.T, tradeOpened, tradesClosed, tradeReduced string) []byte {
	t.Helper()
	type tradeRef struct {
		TradeID string `json:"tradeID"`
	}
	type fillTx struct {
		ID           string     `json:"id"`
		TradeOpened  tradeRef   `json:"tradeOpened"`
		TradesClosed []tradeRef `json:"tradesClosed,omitempty"`
		TradeReduced tradeRef   `json:"tradeReduced,omitempty"`
		Instrument   string     `json:"instrument"`
		Units        string     `json:"units"`
		Price        string     `json:"price"`
	}
	type resp struct {
		OrderFillTransaction  fillTx   `json:"orderFillTransaction"`
		RelatedTransactionIDs []string `json:"relatedTransactionIDs"`
	}

	tx := fillTx{
		ID:         "999",
		Instrument: "EUR_USD",
		Units:      "10000",
		Price:      "1.08500",
	}
	if tradeOpened != "" {
		tx.TradeOpened = tradeRef{TradeID: tradeOpened}
	}
	if tradesClosed != "" {
		tx.TradesClosed = []tradeRef{{TradeID: tradesClosed}}
	}
	if tradeReduced != "" {
		tx.TradeReduced = tradeRef{TradeID: tradeReduced}
	}

	b, err := json.Marshal(resp{OrderFillTransaction: tx})
	require.NoError(t, err)
	return b
}

func newOrderServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
}

// TestSubmitMarketOrder_TradeOpened verifies that a normal long fill (new trade,
// no netting) extracts the tradeID from tradeOpened.
func TestSubmitMarketOrder_TradeOpened(t *testing.T) {
	srv := newOrderServer(t, orderFillBody(t, "111", "", ""))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	result, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08000)
	require.NoError(t, err)
	assert.Equal(t, "111", result.TradeID)
	assert.Equal(t, "999", result.OrderID)
	assert.EqualValues(t, 10000, result.Units)
}

// TestSubmitMarketOrder_TradesClosed verifies that when a short order nets an
// existing long position, the tradeID is taken from tradesClosed[0].
func TestSubmitMarketOrder_TradesClosed(t *testing.T) {
	// tradeOpened is empty — OANDA sends tradesClosed instead.
	srv := newOrderServer(t, orderFillBody(t, "", "222", ""))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	result, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", -10000, 1.09000)
	require.NoError(t, err)
	assert.Equal(t, "222", result.TradeID, "should fall back to tradesClosed[0]")
}

// TestSubmitMarketOrder_TradeReduced verifies fallback to tradeReduced when
// an order partially nets an existing position.
func TestSubmitMarketOrder_TradeReduced(t *testing.T) {
	srv := newOrderServer(t, orderFillBody(t, "", "", "333"))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	result, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", -5000, 1.09000)
	require.NoError(t, err)
	assert.Equal(t, "333", result.TradeID, "should fall back to tradeReduced")
}

// TestSubmitMarketOrder_NoTradeID verifies that when none of the netting fields
// carry a tradeID (should not happen in practice), the field is just empty.
func TestSubmitMarketOrder_NoTradeID(t *testing.T) {
	srv := newOrderServer(t, orderFillBody(t, "", "", ""))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	result, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08000)
	require.NoError(t, err)
	assert.Equal(t, "", result.TradeID)
}

// TestSubmitMarketOrder_HTTPError verifies that a non-201 status is returned as
// an error.
func TestSubmitMarketOrder_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"errorMessage":"bad order"}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// TestSubmitMarketOrder_ZeroUnitsRejected verifies the pre-flight guard.
func TestSubmitMarketOrder_ZeroUnitsRejected(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: "tok"}
	_, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 0, 1.08)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "units must be non-zero")
}

// TestSubmitMarketOrder_FOKCancelled verifies that a 201 response containing
// orderCancelTransaction (no fill) is surfaced as an error, not silent zeros.
func TestSubmitMarketOrder_FOKCancelled(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"orderCancelTransaction": map[string]any{
			"reason": "MARKET_ORDER_FOK_TRANSACTION_REJECTED",
		},
		"relatedTransactionIDs": []string{"99"},
	})
	srv := newOrderServer(t, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
	assert.Contains(t, err.Error(), "MARKET_ORDER_FOK_TRANSACTION_REJECTED")
}

// TestSubmitMarketOrder_OrderRejected verifies that orderRejectTransaction is
// surfaced as an error with the reject reason.
func TestSubmitMarketOrder_OrderRejected(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"orderRejectTransaction": map[string]any{
			"reason":       "MARKET_ORDER_REJECT_TRANSACTION",
			"rejectReason": "STOP_LOSS_ON_FILL_LOSS",
		},
		"relatedTransactionIDs": []string{"100"},
	})
	srv := newOrderServer(t, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected")
	assert.Contains(t, err.Error(), "STOP_LOSS_ON_FILL_LOSS")
}

// TestSubmitMarketOrder_NoFillNoReason verifies the fallback error when the
// response is a 201 with no fill, cancel, or reject transaction.
func TestSubmitMarketOrder_NoFillNoReason(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"relatedTransactionIDs": []string{"101"},
	})
	srv := newOrderServer(t, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.SubmitMarketOrder(context.Background(), "ACC1", "EUR_USD", 10000, 1.08)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not filled")
}
