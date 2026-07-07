package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// AccountSummary holds the key financial fields from OANDA's account summary.
type AccountSummary struct {
	ID           string
	Alias        string // human-readable account name (OANDA "alias")
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
		Alias        string `json:"alias"`
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

	a := resp.Account
	balance, err := parseOptionalFloatField("account balance", a.Balance)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	nav, err := parseOptionalFloatField("account NAV", a.NAV)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	unrealizedPL, err := parseOptionalFloatField("account unrealizedPL", a.UnrealizedPL)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	marginUsed, err := parseOptionalFloatField("account marginUsed", a.MarginUsed)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	marginAvail, err := parseOptionalFloatField("account marginAvailable", a.MarginAvail)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}

	return &AccountSummary{
		ID:           a.ID,
		Alias:        a.Alias,
		Currency:     a.Currency,
		Balance:      balance,
		NAV:          nav,
		UnrealizedPL: unrealizedPL,
		MarginUsed:   marginUsed,
		MarginAvail:  marginAvail,
	}, nil
}
