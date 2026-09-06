package alpaca

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// fakeAlpacaServer is a minimal, in-memory stand-in for Alpaca's paper
// Trading API, implementing HTTPDoer directly (no real listener, no
// real network — every test in this package stays hermetic). It exists
// to exercise Broker/accountHandle end-to-end against a stateful,
// order-lifecycle-aware fake, which the plain request/response
// fakeDoer in client_test.go is not designed for.
type fakeAlpacaServer struct {
	mu      sync.Mutex
	account wireAccount
	// positions is keyed by symbol.
	positions map[string]wirePosition
	// orders is keyed by Alpaca's own native order id.
	orders map[string]wireOrder
	nextID int
	// autoFill, when true, makes SubmitOrder immediately report the
	// order as filled (simulating a market order that executes within
	// the request/response window) instead of leaving it "new".
	autoFill bool
}

func newFakeAlpacaServer() *fakeAlpacaServer {
	return &fakeAlpacaServer{
		account: wireAccount{
			Currency: "USD", Cash: "100000", Equity: "100000",
			BuyingPower: "200000", InitialMargin: "0", MaintenanceMargin: "0",
		},
		positions: make(map[string]wirePosition),
		orders:    make(map[string]wireOrder),
	}
}

func (s *fakeAlpacaServer) Do(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v2/account":
		return jsonResponse(200, s.account)
	case req.Method == http.MethodGet && req.URL.Path == "/v2/positions":
		out := make([]wirePosition, 0, len(s.positions))
		for _, p := range s.positions {
			out = append(out, p)
		}
		return jsonResponse(200, out)
	case req.Method == http.MethodPost && req.URL.Path == "/v2/orders":
		return s.submitOrder(req)
	case req.Method == http.MethodGet && req.URL.Path == "/v2/orders:by_client_order_id":
		return s.getByClientOrderID(req)
	case req.Method == http.MethodGet && req.URL.Path == "/v2/orders":
		return s.listOrders(req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v2/orders/"):
		return s.getOrder(strings.TrimPrefix(req.URL.Path, "/v2/orders/"))
	case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/v2/orders/"):
		return s.cancelOrder(strings.TrimPrefix(req.URL.Path, "/v2/orders/"))
	case req.Method == http.MethodPatch && strings.HasPrefix(req.URL.Path, "/v2/orders/"):
		return s.replaceOrder(strings.TrimPrefix(req.URL.Path, "/v2/orders/"), req)
	default:
		return jsonResponse(404, map[string]string{"message": "not found: " + req.Method + " " + req.URL.Path})
	}
}

func (s *fakeAlpacaServer) submitOrder(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var sub wireOrderSubmit
	if err := json.Unmarshal(body, &sub); err != nil {
		return jsonResponse(400, map[string]string{"message": "bad request"})
	}

	// Idempotency: resubmitting the same client_order_id returns the
	// existing order, matching Alpaca's real documented behavior.
	for _, o := range s.orders {
		if o.ClientOrderID == sub.ClientOrderID {
			return jsonResponse(200, o)
		}
	}

	s.nextID++
	id := "alpaca-" + strconv.Itoa(s.nextID)
	status := "new"
	filledQty := "0"
	var filledAvgPrice *string
	if s.autoFill {
		status = "filled"
		filledQty = sub.Qty
		price := "100.00"
		filledAvgPrice = &price
	}
	wo := wireOrder{
		ID: id, ClientOrderID: sub.ClientOrderID, Symbol: sub.Symbol,
		Qty: sub.Qty, FilledQty: filledQty, FilledAvgPrice: filledAvgPrice,
		Type: sub.Type, Side: sub.Side, TimeInForce: sub.TimeInForce,
		LimitPrice: sub.LimitPrice, StopPrice: sub.StopPrice, Status: status,
	}
	s.orders[id] = wo
	return jsonResponse(200, wo)
}

func (s *fakeAlpacaServer) getByClientOrderID(req *http.Request) (*http.Response, error) {
	clientOrderID := req.URL.Query().Get("client_order_id")
	for _, o := range s.orders {
		if o.ClientOrderID == clientOrderID {
			return jsonResponse(200, o)
		}
	}
	return jsonResponse(404, map[string]string{"message": "order not found"})
}

func (s *fakeAlpacaServer) getOrder(id string) (*http.Response, error) {
	id, _ = url.PathUnescape(id)
	o, ok := s.orders[id]
	if !ok {
		return jsonResponse(404, map[string]string{"message": "order not found"})
	}
	return jsonResponse(200, o)
}

func (s *fakeAlpacaServer) listOrders(req *http.Request) (*http.Response, error) {
	status := req.URL.Query().Get("status")
	out := make([]wireOrder, 0, len(s.orders))
	for _, o := range s.orders {
		switch status {
		case "", "open":
			if !isTerminalWireStatus(o.Status) {
				out = append(out, o)
			}
		case "closed":
			if isTerminalWireStatus(o.Status) {
				out = append(out, o)
			}
		case "all":
			out = append(out, o)
		}
	}
	return jsonResponse(200, out)
}

func isTerminalWireStatus(s string) bool {
	switch s {
	case "filled", "canceled", "rejected", "expired":
		return true
	default:
		return false
	}
}

func (s *fakeAlpacaServer) cancelOrder(id string) (*http.Response, error) {
	id, _ = url.PathUnescape(id)
	o, ok := s.orders[id]
	if !ok {
		return jsonResponse(404, map[string]string{"message": "order not found"})
	}
	if isTerminalWireStatus(o.Status) {
		return jsonResponse(422, map[string]string{"message": "order not cancelable"})
	}
	o.Status = "pending_cancel"
	s.orders[id] = o
	return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
}

// settleCancel simulates Alpaca finishing an asynchronous cancel:
// call after cancelOrder to move the order from pending_cancel to
// canceled, as the polling EventReader would observe on its next poll.
func (s *fakeAlpacaServer) settleCancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[id]
	o.Status = "canceled"
	s.orders[id] = o
}

// settleFill simulates Alpaca reporting a market order as filled some
// time after submission (rather than fakeAlpacaServer.autoFill's
// immediate-fill shortcut), for tests that need to observe the
// EventReader's Working -> Filled transition and synthesized Fill.
func (s *fakeAlpacaServer) settleFill(id, filledQty, avgPrice string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[id]
	o.Status = "filled"
	o.FilledQty = filledQty
	o.FilledAvgPrice = &avgPrice
	s.orders[id] = o
}

func (s *fakeAlpacaServer) replaceOrder(id string, req *http.Request) (*http.Response, error) {
	id, _ = url.PathUnescape(id)
	o, ok := s.orders[id]
	if !ok {
		return jsonResponse(404, map[string]string{"message": "order not found"})
	}
	if isTerminalWireStatus(o.Status) {
		return jsonResponse(422, map[string]string{"message": "order not replaceable"})
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var rep wireOrderReplace
	if err := json.Unmarshal(body, &rep); err != nil {
		return jsonResponse(400, map[string]string{"message": "bad request"})
	}
	if rep.Qty != nil {
		o.Qty = *rep.Qty
	}
	if rep.LimitPrice != nil {
		o.LimitPrice = rep.LimitPrice
	}
	if rep.StopPrice != nil {
		o.StopPrice = rep.StopPrice
	}
	o.Status = "pending_replace"
	s.orders[id] = o
	return jsonResponse(200, o)
}

func jsonResponse(status int, v any) (*http.Response, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("fakeAlpacaServer: marshal response: %w", err)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}
