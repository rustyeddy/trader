package alpaca

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake HTTP transport: no test in this file makes a real network call ---
// Mirrors marketdata/internal/provider/alpaca/client_test.go's identical
// fakeDoer shape.

type fakeResponse struct {
	status int
	body   string
	err    error
}

type fakeDoer struct {
	mu        sync.Mutex
	responses []fakeResponse
	requests  []*http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeDoer: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

func (f *fakeDoer) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func testClientConfig(doer HTTPDoer) ClientConfig {
	return ClientConfig{
		BaseURL:    DefaultPaperBaseURL,
		Credential: StaticCredential{KeyID: "key", SecretKey: "secret"},
		HTTPClient: doer,
	}
}

func TestNewClient_RequiresBaseURLAndCredential(t *testing.T) {
	_, err := NewClient(ClientConfig{Credential: StaticCredential{}})
	require.Error(t, err)
	_, err = NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL})
	require.Error(t, err)
}

func TestGetAccount_Success(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: `{
		"currency": "USD", "cash": "100000", "equity": "100000",
		"buying_power": "200000", "initial_margin": "0", "maintenance_margin": "0"
	}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	acct, err := c.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "USD", acct.Currency)
	assert.Equal(t, "100000", acct.Cash)

	req := doer.lastRequest()
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "/v2/account", req.URL.Path)
	assert.Equal(t, "key", req.Header.Get("APCA-API-KEY-ID"))
	assert.Equal(t, "secret", req.Header.Get("APCA-API-SECRET-KEY"))
}

func TestGetAccount_Unauthorized(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 401, body: `{"message":"unauthorized"}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	_, err = c.GetAccount(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestListPositions_Success(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: `[
		{"symbol":"AAPL","side":"long","qty":"10","avg_entry_price":"150.00","unrealized_pl":"5.00"}
	]`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	positions, err := c.ListPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "AAPL", positions[0].Symbol)
	assert.Equal(t, "long", positions[0].Side)
}

func TestSubmitOrder_Success(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: `{
		"id":"alpaca-1","client_order_id":"ord_abc","symbol":"AAPL","qty":"10",
		"filled_qty":"0","type":"market","side":"buy","time_in_force":"day","status":"new"
	}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	wo, err := c.SubmitOrder(context.Background(), wireOrderSubmit{
		Symbol: "AAPL", Qty: "10", Side: "buy", Type: "market", TimeInForce: "day", ClientOrderID: "ord_abc",
	})
	require.NoError(t, err)
	assert.Equal(t, "alpaca-1", wo.ID)

	req := doer.lastRequest()
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/v2/orders", req.URL.Path)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestSubmitOrder_Rejected(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 422, body: `{"message":"insufficient buying power"}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	_, err = c.SubmitOrder(context.Background(), wireOrderSubmit{Symbol: "AAPL", Qty: "10", Side: "buy", Type: "market", TimeInForce: "day", ClientOrderID: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderRejected)
}

func TestGetOrder_NotFound(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 404, body: `{"message":"order not found"}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	_, err = c.GetOrder(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

func TestListOrders_BuildsQuery(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: `[]`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	_, err = c.ListOrders(context.Background(), ListOrdersOptions{Status: "all", Limit: 50})
	require.NoError(t, err)

	req := doer.lastRequest()
	assert.Equal(t, "all", req.URL.Query().Get("status"))
	assert.Equal(t, "50", req.URL.Query().Get("limit"))
	assert.Equal(t, "asc", req.URL.Query().Get("direction"))
}

func TestCancelOrder_Success(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 204, body: ""}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	err = c.CancelOrder(context.Background(), "alpaca-1")
	require.NoError(t, err)

	req := doer.lastRequest()
	assert.Equal(t, "DELETE", req.Method)
	assert.Equal(t, "/v2/orders/alpaca-1", req.URL.Path)
}

func TestCancelOrder_NotFound(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 404, body: `{}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	err = c.CancelOrder(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

func TestReplaceOrder_Success(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: `{
		"id":"alpaca-1","client_order_id":"ord_abc","symbol":"AAPL","qty":"5",
		"filled_qty":"0","type":"market","side":"buy","time_in_force":"day","status":"pending_replace"
	}`}}}
	c, err := NewClient(testClientConfig(doer))
	require.NoError(t, err)

	qty := "5"
	wo, err := c.ReplaceOrder(context.Background(), "alpaca-1", wireOrderReplace{Qty: &qty})
	require.NoError(t, err)
	assert.Equal(t, "5", wo.Qty)

	req := doer.lastRequest()
	assert.Equal(t, "PATCH", req.Method)
}

func TestClient_RetriesTransientThenSucceeds(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 503, body: `{}`},
		{status: 200, body: `{"currency":"USD","cash":"1","equity":"1","buying_power":"1","initial_margin":"0","maintenance_margin":"0"}`},
	}}
	c, err := NewClient(ClientConfig{
		BaseURL:        DefaultPaperBaseURL,
		Credential:     StaticCredential{KeyID: "k", SecretKey: "s"},
		HTTPClient:     doer,
		RetryBaseDelay: 1,
	})
	require.NoError(t, err)

	_, err = c.GetAccount(context.Background())
	require.NoError(t, err)
}

func TestClient_RetriesExhausted(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 503, body: `{}`}, {status: 503, body: `{}`}, {status: 503, body: `{}`},
	}}
	c, err := NewClient(ClientConfig{
		BaseURL:        DefaultPaperBaseURL,
		Credential:     StaticCredential{KeyID: "k", SecretKey: "s"},
		HTTPClient:     doer,
		RetryBaseDelay: 1,
		MaxAttempts:    3,
	})
	require.NoError(t, err)

	_, err = c.GetAccount(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
}

func TestClient_NetworkErrorIsRetried(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{err: errors.New("connection refused")},
		{status: 200, body: `{"currency":"USD","cash":"1","equity":"1","buying_power":"1","initial_margin":"0","maintenance_margin":"0"}`},
	}}
	c, err := NewClient(ClientConfig{
		BaseURL:        DefaultPaperBaseURL,
		Credential:     StaticCredential{KeyID: "k", SecretKey: "s"},
		HTTPClient:     doer,
		RetryBaseDelay: 1,
	})
	require.NoError(t, err)

	_, err = c.GetAccount(context.Background())
	require.NoError(t, err)
}
