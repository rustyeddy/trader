package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// AccountRef is a minimal account descriptor returned by GET /v3/accounts.
type AccountRef struct {
	ID   string `json:"id"`
	Tags []string `json:"tags"`
}

type accountsResp struct {
	Accounts []AccountRef `json:"accounts"`
}

// GetAccounts returns all account IDs associated with the token.
func (c *Client) GetAccounts(ctx context.Context) ([]AccountRef, error) {
	body, err := c.Get(ctx, "/v3/accounts", nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var resp accountsResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("oanda: parse accounts response: %w", err)
	}
	return resp.Accounts, nil
}
