package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Transaction is one OANDA account transaction. We promote the fields we
// care about for journaling and reconciliation; the raw JSON is kept so
// callers can dig into transaction-type-specific fields without us modeling
// every variant.
type Transaction struct {
	ID        string    // OANDA transaction ID (numeric, monotonically increasing)
	BatchID   string    // groups related transactions from a single API request
	AccountID string
	Type      string    // e.g. "ORDER_FILL", "MARKET_ORDER", "STOP_LOSS_FILLED"
	Time      time.Time
	Reason    string

	// Order / fill specific — zero/empty for non-fill transactions.
	Instrument     string
	Units          int64   // signed: positive long, negative short
	Price          float64 // execution price for fills; order price for pending orders
	PL             float64 // realized P/L for ORDER_FILL closes
	AccountBalance float64 // post-transaction balance (for funding/fills)
	OrderID        string  // the order this fill executed against
	TradeID        string  // the trade opened by this fill (when applicable)

	// TradesClosed is populated on ORDER_FILL transactions that close one
	// or more existing trades. Empty for open fills.
	TradesClosed []ClosedTrade

	// Full transaction payload — use for transaction-type-specific fields
	// the typed struct doesn't expose (financing breakdowns, margin call
	// info, etc.).
	Raw json.RawMessage
}

// ClosedTrade is one entry inside an ORDER_FILL's tradesClosed array.
// A single closing order can close multiple trades (e.g. closing a
// position that's the aggregate of several legs).
type ClosedTrade struct {
	TradeID    string
	Units      int64   // signed; the units that were closed (opposite of open direction)
	Price      float64 // close price
	RealizedPL float64 // realized P/L in account currency
}

type sinceIDResp struct {
	Transactions      []json.RawMessage `json:"transactions"`
	LastTransactionID string            `json:"lastTransactionID"`
}

// GetTransactions returns every transaction with ID strictly greater than
// sinceID. Use sinceID=0 to fetch from the beginning. Returns the
// transactions and the new lastTransactionID (suitable for the next poll).
//
// OANDA caps a single response at 1000 transactions. If you get back
// exactly 1000, call again with the new lastID to get the next page.
func (c *Client) GetTransactions(ctx context.Context, accountID string, sinceID int64) ([]Transaction, int64, error) {
	if c.Token == "" {
		return nil, 0, fmt.Errorf("oanda: missing token")
	}
	if accountID == "" {
		return nil, 0, fmt.Errorf("oanda: missing account ID")
	}

	body, err := c.Get(ctx,
		fmt.Sprintf("/v3/accounts/%s/transactions/sinceid", accountID),
		map[string]string{"id": strconv.FormatInt(sinceID, 10)},
	)
	if err != nil {
		return nil, 0, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, 0, err
	}

	var resp sinceIDResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("oanda: parse transactions: %w", err)
	}

	out := make([]Transaction, 0, len(resp.Transactions))
	for _, raw := range resp.Transactions {
		t, err := parseTransaction(raw)
		if err != nil {
			return out, 0, fmt.Errorf("oanda: parse transaction: %w", err)
		}
		out = append(out, t)
	}

	lastID, _ := strconv.ParseInt(resp.LastTransactionID, 10, 64)
	return out, lastID, nil
}

// parseTransaction decodes one transaction JSON object into the Transaction
// struct, promoting the commonly-useful fields. Fields that aren't present
// in a given transaction type (e.g. Price on a CREATE event) are left zero.
func parseTransaction(raw json.RawMessage) (Transaction, error) {
	var v struct {
		ID             string `json:"id"`
		BatchID        string `json:"batchID"`
		AccountID      string `json:"accountID"`
		Type           string `json:"type"`
		Time           string `json:"time"`
		Reason         string `json:"reason"`
		Instrument     string `json:"instrument"`
		Units          string `json:"units"`
		Price          string `json:"price"`
		PL             string `json:"pl"`
		AccountBalance string `json:"accountBalance"`
		OrderID        string `json:"orderID"`
		TradeOpened    *struct {
			TradeID string `json:"tradeID"`
		} `json:"tradeOpened"`
		TradesClosed []struct {
			TradeID    string `json:"tradeID"`
			Units      string `json:"units"`
			Price      string `json:"price"`
			RealizedPL string `json:"realizedPL"`
		} `json:"tradesClosed"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Transaction{}, err
	}

	t := Transaction{
		ID:        v.ID,
		BatchID:   v.BatchID,
		AccountID: v.AccountID,
		Type:      v.Type,
		Reason:    v.Reason,
		Instrument: v.Instrument,
		OrderID:   v.OrderID,
		Raw:       raw,
	}

	if v.Time != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v.Time); err == nil {
			t.Time = ts
		}
	}
	if v.Units != "" {
		t.Units, _ = strconv.ParseInt(v.Units, 10, 64)
	}
	if v.Price != "" {
		t.Price, _ = strconv.ParseFloat(v.Price, 64)
	}
	if v.PL != "" {
		t.PL, _ = strconv.ParseFloat(v.PL, 64)
	}
	if v.AccountBalance != "" {
		t.AccountBalance, _ = strconv.ParseFloat(v.AccountBalance, 64)
	}
	if v.TradeOpened != nil {
		t.TradeID = v.TradeOpened.TradeID
	}
	if len(v.TradesClosed) > 0 {
		t.TradesClosed = make([]ClosedTrade, len(v.TradesClosed))
		for i, tc := range v.TradesClosed {
			closed := ClosedTrade{TradeID: tc.TradeID}
			closed.Units, _ = strconv.ParseInt(tc.Units, 10, 64)
			closed.Price, _ = strconv.ParseFloat(tc.Price, 64)
			closed.RealizedPL, _ = strconv.ParseFloat(tc.RealizedPL, 64)
			t.TradesClosed[i] = closed
		}
	}

	return t, nil
}
