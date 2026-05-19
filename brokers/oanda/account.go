package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// AccountSummary holds the key financial fields from OANDA's account summary.
type AccountSummary struct {
	ID           string
	Currency     string
	Balance      float64
	NAV          float64 // Net Asset Value (equity)
	UnrealizedPL float64
	MarginUsed   float64
	MarginAvail  float64
}

type accountSummaryResp struct {
	Account struct {
		ID           string `json:"id"`
		Currency     string `json:"currency"`
		Balance      string `json:"balance"`
		NAV          string `json:"NAV"`
		UnrealizedPL string `json:"unrealizedPL"`
		MarginUsed   string `json:"marginUsed"`
		MarginAvail  string `json:"marginAvailable"`
	} `json:"account"`
}

// GetAccountSummary fetches balance, equity, and margin from OANDA.
func (c *Client) GetAccountSummary(ctx context.Context, accountID string) (*AccountSummary, error) {
	body, err := c.Get(ctx, fmt.Sprintf("/v3/accounts/%s/summary", accountID), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var resp accountSummaryResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("oanda: parse account summary: %w", err)
	}

	parse := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}

	a := resp.Account
	return &AccountSummary{
		ID:           a.ID,
		Currency:     a.Currency,
		Balance:      parse(a.Balance),
		NAV:          parse(a.NAV),
		UnrealizedPL: parse(a.UnrealizedPL),
		MarginUsed:   parse(a.MarginUsed),
		MarginAvail:  parse(a.MarginAvail),
	}, nil
}
