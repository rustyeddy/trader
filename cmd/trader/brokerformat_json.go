package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/rustyeddy/trader/account"
	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// brokerJSONFormatter renders a stable, structured JSON document per
// response, mirroring jsonFormatter's own convention (format_json.go):
// small, unexported view types instead of encoding domain/response
// values directly, so adding JSON support never requires a
// MarshalJSON method on a domain type purely to satisfy this
// transport's presentation needs.
type brokerJSONFormatter struct{}

func (brokerJSONFormatter) encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type jsonAccountRef struct {
	AccountID string `json:"account_id"`
	Broker    string `json:"broker"`
}

func (f brokerJSONFormatter) FormatAccounts(w io.Writer, resp svcbroker.AccountsResponse) error {
	refs := make([]jsonAccountRef, len(resp.Accounts))
	for i, ref := range resp.Accounts {
		refs[i] = jsonAccountRef{AccountID: ref.AccountID.String(), Broker: ref.Broker}
	}
	return f.encode(w, struct {
		Accounts []jsonAccountRef `json:"accounts"`
	}{Accounts: refs})
}

type jsonPosition struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`
	Quantity string `json:"quantity"`
	AvgPrice string `json:"avg_price"`
}

type jsonOpenOrder struct {
	OrderID  string `json:"order_id"`
	Symbol   string `json:"symbol"`
	Status   string `json:"status"`
	Quantity string `json:"quantity"`
}

type jsonSnapshot struct {
	AccountID     string          `json:"account_id"`
	Broker        string          `json:"broker"`
	AsOf          time.Time       `json:"as_of"`
	Cash          string          `json:"cash"`
	Equity        string          `json:"equity"`
	RealizedPnL   string          `json:"realized_pnl"`
	UnrealizedPnL string          `json:"unrealized_pnl"`
	Fees          string          `json:"fees"`
	Positions     []jsonPosition  `json:"positions"`
	OpenOrders    []jsonOpenOrder `json:"open_orders"`
}

func toJSONSnapshot(snap account.Snapshot) jsonSnapshot {
	positions := make([]jsonPosition, len(snap.Positions()))
	for i, p := range snap.Positions() {
		positions[i] = jsonPosition{Symbol: p.Listing.Symbol(), Side: p.Side.String(), Quantity: p.Quantity.String(), AvgPrice: p.AvgPrice.String()}
	}
	orders := make([]jsonOpenOrder, len(snap.OpenOrders()))
	for i, o := range snap.OpenOrders() {
		orders[i] = jsonOpenOrder{OrderID: o.Request.OrderID.String(), Symbol: o.Request.Listing.Symbol(), Status: o.Status.String(), Quantity: o.Request.Quantity.String()}
	}
	return jsonSnapshot{
		AccountID:     snap.AccountID().String(),
		Broker:        snap.Broker(),
		AsOf:          snap.AsOf(),
		Cash:          firstCashBalance(snap),
		Equity:        snap.Equity().String(),
		RealizedPnL:   snap.RealizedPnL().String(),
		UnrealizedPnL: snap.UnrealizedPnL().String(),
		Fees:          snap.Fees().String(),
		Positions:     positions,
		OpenOrders:    orders,
	}
}

func (f brokerJSONFormatter) FormatSnapshot(w io.Writer, resp svcbroker.SnapshotResponse) error {
	return f.encode(w, toJSONSnapshot(resp.Snapshot))
}

type jsonSubmitResult struct {
	OrderID       string `json:"order_id"`
	BrokerOrderID string `json:"broker_order_id"`
	Status        string `json:"status"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	FilledQty     string `json:"filled_quantity"`
}

func (f brokerJSONFormatter) FormatSubmit(w io.Writer, resp svcbroker.SubmitResponse) error {
	o := resp.Order
	return f.encode(w, jsonSubmitResult{
		OrderID:       o.Request.OrderID.String(),
		BrokerOrderID: o.BrokerOrderID,
		Status:        o.Status.String(),
		Symbol:        o.Request.Listing.Symbol(),
		Side:          o.Request.Side.String(),
		FilledQty:     o.FilledQuantity.String(),
	})
}
