package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Price holds the current bid and ask for one instrument.
type Price struct {
	Instrument string
	Bid        float64
	Ask        float64
	Mid        float64
}

type pricingResp struct {
	Prices []struct {
		Instrument string `json:"instrument"`
		Bids       []struct {
			Price string `json:"price"`
		} `json:"bids"`
		Asks []struct {
			Price string `json:"price"`
		} `json:"asks"`
	} `json:"prices"`
}

// GetPricing fetches the current bid/ask for one or more instruments.
// instrument should be OANDA format e.g. "USD_JPY".
func (c *Client) GetPricing(ctx context.Context, accountID string, instruments ...string) ([]Price, error) {
	if len(instruments) == 0 {
		return nil, fmt.Errorf("oanda: GetPricing requires at least one instrument")
	}
	instr := ""
	for i, inst := range instruments {
		if i > 0 {
			instr += ","
		}
		instr += inst
	}

	body, err := c.Get(ctx, fmt.Sprintf("/v3/accounts/%s/pricing", accountID), map[string]string{
		"instruments": instr,
	})
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var resp pricingResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("oanda: parse pricing response: %w", err)
	}

	out := make([]Price, 0, len(resp.Prices))
	for _, p := range resp.Prices {
		if len(p.Bids) == 0 || len(p.Asks) == 0 {
			continue
		}
		bid, err := strconv.ParseFloat(p.Bids[0].Price, 64)
		if err != nil {
			return nil, fmt.Errorf("oanda: parse bid %q: %w", p.Bids[0].Price, err)
		}
		ask, err := strconv.ParseFloat(p.Asks[0].Price, 64)
		if err != nil {
			return nil, fmt.Errorf("oanda: parse ask %q: %w", p.Asks[0].Price, err)
		}
		out = append(out, Price{
			Instrument: p.Instrument,
			Bid:        bid,
			Ask:        ask,
			Mid:        (bid + ask) / 2,
		})
	}
	return out, nil
}
