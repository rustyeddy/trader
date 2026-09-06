package alpaca

// This file isolates every assumption about Alpaca's Trading API wire
// shape, the same discipline
// marketdata/internal/provider/alpaca/wireshape.go established for the
// Market Data API (ADR-050): if a real paper account's response turns
// out to disagree with what is decoded here, this is the one place that
// needs correcting.
//
// # Confidence
//
// The endpoints, HTTP methods, and top-level request/response shape
// below are Alpaca's real, published Trading API v2 surface — safe to
// treat as fact (see client.go's endpoint-level comments). What is
// genuinely unverified in this implementation is the exact set of field
// names decoded here (this package decodes only the fields Phase 1
// needs, so an unrecognized additional field is simply ignored, never a
// decode error) and two specific open questions flagged inline below:
// whether Alpaca's replace endpoint preserves an order's original id or
// mints a new one, and the exact set of HTTP statuses Alpaca uses to
// report an order-level rejection (insufficient buying power, invalid
// quantity) as opposed to a transport/auth failure. Both are resolved
// defensively rather than guessed at — see replaceOrder in client.go and
// classifyOrderStatus below.

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// wireAccount is GET /v2/account's decoded response shape. Only the
// fields account.SnapshotParams needs are decoded.
type wireAccount struct {
	Currency          string `json:"currency"`
	Cash              string `json:"cash"`
	Equity            string `json:"equity"`
	BuyingPower       string `json:"buying_power"`
	InitialMargin     string `json:"initial_margin"`
	MaintenanceMargin string `json:"maintenance_margin"`
}

// wirePosition is one entry of GET /v2/positions's decoded response
// shape. qty is Alpaca's own non-negative decimal string; direction is
// carried separately in side ("long"/"short"), mirroring
// order.Position's own Quantity-always-non-negative/PositionSide split
// exactly, so no sign juggling is needed translating one into the other.
type wirePosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	AvgEntryPrice string `json:"avg_entry_price"`
	UnrealizedPL  string `json:"unrealized_pl"`
}

// wireOrder is one order's decoded shape, returned by POST /v2/orders,
// GET /v2/orders/{id}, GET /v2/orders (list), PATCH /v2/orders/{id}
// (replace), and DELETE /v2/orders/{id} on decline (this package always
// re-fetches after a declined cancel; see cancelOrder in client.go).
type wireOrder struct {
	ID             string  `json:"id"`
	ClientOrderID  string  `json:"client_order_id"`
	Symbol         string  `json:"symbol"`
	Qty            string  `json:"qty"`
	FilledQty      string  `json:"filled_qty"`
	FilledAvgPrice *string `json:"filled_avg_price"`
	Type           string  `json:"type"`
	Side           string  `json:"side"`
	TimeInForce    string  `json:"time_in_force"`
	LimitPrice     *string `json:"limit_price"`
	StopPrice      *string `json:"stop_price"`
	Status         string  `json:"status"`
}

// wireOrderSubmit is POST /v2/orders's request body shape. Only Market
// orders are constructed by this package (see translate.go), so
// LimitPrice/StopPrice are never set — the fields exist so a future,
// additive Limit/Stop extension does not need a second request type.
type wireOrderSubmit struct {
	Symbol        string  `json:"symbol"`
	Qty           string  `json:"qty"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	TimeInForce   string  `json:"time_in_force"`
	ClientOrderID string  `json:"client_order_id"`
	LimitPrice    *string `json:"limit_price,omitempty"`
	StopPrice     *string `json:"stop_price,omitempty"`
}

// wireOrderReplace is PATCH /v2/orders/{id}'s request body shape. Every
// field is optional; only non-nil fields are sent.
type wireOrderReplace struct {
	Qty        *string `json:"qty,omitempty"`
	LimitPrice *string `json:"limit_price,omitempty"`
	StopPrice  *string `json:"stop_price,omitempty"`
}

// classifyStatus maps an HTTP status code from a non-order-scoped
// endpoint (account, positions, order list, order submit) to one of
// Client's sentinel errors, including a body excerpt for diagnostics —
// never a request header, so neither credential secret can leak into an
// error message via this path. Mirrors
// marketdata/internal/provider/alpaca's classifyStatus exactly for the
// status classes it shares.
func classifyStatus(status int, body io.Reader) error {
	excerpt := readExcerpt(body)
	switch {
	case status == 401 || status == 403:
		return fmt.Errorf("%w: http %d: %s", ErrUnauthorized, status, excerpt)
	case status == 400 || status == 404:
		return fmt.Errorf("%w: http %d: %s", ErrBadRequest, status, excerpt)
	case status == 422:
		// Alpaca's real, documented use of 422 for POST /v2/orders is an
		// order-level rejection (insufficient buying power, invalid
		// quantity, market closed for the requested time-in-force) — but
		// the exact set of statuses Alpaca uses for every rejection case
		// is not verified in this implementation (see the file doc
		// comment). Submit treats this sentinel specifically, converting
		// it into a StatusRejected order.Order rather than a plain Go
		// error; every other classification here propagates as an error.
		return fmt.Errorf("%w: http %d: %s", ErrOrderRejected, status, excerpt)
	case status == 429:
		return fmt.Errorf("%w: http %d: %s", ErrRateLimited, status, excerpt)
	case status >= 500:
		return fmt.Errorf("%w: http %d: %s", ErrProviderUnavailable, status, excerpt)
	default:
		return fmt.Errorf("%w: http %d: %s", ErrUnexpectedStatus, status, excerpt)
	}
}

// classifyOrderStatus is classifyStatus's counterpart for an
// order-scoped endpoint (get/cancel/replace one order by id): 404 means
// the order id itself is unrecognized (ErrOrderNotFound), distinct from
// classifyStatus's own 404 handling (ErrBadRequest, for an endpoint with
// no single order id to be "not found"). Every other status classifies
// identically to classifyStatus.
func classifyOrderStatus(status int, body io.Reader) error {
	if status == 404 {
		excerpt := readExcerpt(body)
		return fmt.Errorf("%w: http %d: %s", ErrOrderNotFound, status, excerpt)
	}
	return classifyStatus(status, body)
}

func readExcerpt(body io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(body, 4*1024))
	return strings.TrimSpace(string(b))
}

// isTransient reports whether err should be retried.
func isTransient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, errNetwork)
}
