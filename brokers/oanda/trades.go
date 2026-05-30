package oanda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// OpenTrade represents one open position returned by GET /v3/accounts/{id}/openTrades.
type OpenTrade struct {
	ID           string
	Instrument   string
	EntryPrice   float64
	Units        int64   // positive = long, negative = short
	UnrealizedPL float64
	StopLoss     float64   // 0 if none
	TakeProfit   float64   // 0 if none
	OpenTime     time.Time // when the trade was opened on OANDA
}

type openTradesResp struct {
	Trades []struct {
		ID           string `json:"id"`
		Instrument   string `json:"instrument"`
		Price        string `json:"price"`
		CurrentUnits string `json:"currentUnits"`
		UnrealizedPL string `json:"unrealizedPL"`
		OpenTime     string `json:"openTime"` // RFC3339Nano, e.g. "2024-01-15T10:30:00.000000Z"
		StopLossOrder *struct {
			Price string `json:"price"`
		} `json:"stopLossOrder"`
		TakeProfitOrder *struct {
			Price string `json:"price"`
		} `json:"takeProfitOrder"`
	} `json:"trades"`
}

// GetOpenTrades returns all open trades for the account.
func (c *Client) GetOpenTrades(ctx context.Context, accountID string) ([]OpenTrade, error) {
	body, err := c.Get(ctx, fmt.Sprintf("/v3/accounts/%s/openTrades", accountID), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var resp openTradesResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("oanda: parse open trades: %w", err)
	}

	parse := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}

	out := make([]OpenTrade, 0, len(resp.Trades))
	for _, t := range resp.Trades {
		ot := OpenTrade{
			ID:           t.ID,
			Instrument:   t.Instrument,
			EntryPrice:   parse(t.Price),
			Units:        int64(parse(t.CurrentUnits)),
			UnrealizedPL: parse(t.UnrealizedPL),
		}
		if t.OpenTime != "" {
			if ts, err := time.Parse(time.RFC3339Nano, t.OpenTime); err == nil {
				ot.OpenTime = ts
			}
		}
		if t.StopLossOrder != nil {
			ot.StopLoss = parse(t.StopLossOrder.Price)
		}
		if t.TakeProfitOrder != nil {
			ot.TakeProfit = parse(t.TakeProfitOrder.Price)
		}
		out = append(out, ot)
	}
	return out, nil
}

// CloseTradeResult holds the fill details from a trade close.
type CloseTradeResult struct {
	OrderID string
	TradeID string
	Units   int64
	Price   float64
}

type closeTradeReq struct {
	Units string `json:"units,omitempty"` // omit for full close
}

type closeTradeResp struct {
	OrderFillTransaction struct {
		ID          string `json:"id"`
		TradesClosed []struct {
			TradeID string `json:"tradeID"`
			Units   string `json:"units"`
		} `json:"tradesClosed"`
		Price string `json:"price"`
	} `json:"orderFillTransaction"`
}

// CloseTrade closes an open trade fully (units=0) or partially (units>0).
func (c *Client) CloseTrade(ctx context.Context, accountID, tradeID string, units int64) (*CloseTradeResult, error) {
	var body []byte
	var err error
	if units > 0 {
		body, err = json.Marshal(closeTradeReq{Units: strconv.FormatInt(units, 10)})
	} else {
		body, err = json.Marshal(closeTradeReq{}) // full close
	}
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = fmt.Sprintf("/v3/accounts/%s/trades/%s/close", accountID, tradeID)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oanda: close trade http %d: %s", resp.StatusCode, trimForErr(string(respData)))
	}

	var cr closeTradeResp
	if err := json.Unmarshal(respData, &cr); err != nil {
		return nil, fmt.Errorf("oanda: parse close trade response: %w", err)
	}

	result := &CloseTradeResult{
		OrderID: cr.OrderFillTransaction.ID,
		Price:   func() float64 { v, _ := strconv.ParseFloat(cr.OrderFillTransaction.Price, 64); return v }(),
	}
	if len(cr.OrderFillTransaction.TradesClosed) > 0 {
		tc := cr.OrderFillTransaction.TradesClosed[0]
		result.TradeID = tc.TradeID
		result.Units, _ = strconv.ParseInt(tc.Units, 10, 64)
	}
	return result, nil
}

type updateTradeOrdersReq struct {
	StopLoss   *tradeOrderSpec `json:"stopLoss,omitempty"`
	TakeProfit *tradeOrderSpec `json:"takeProfit,omitempty"`
}

type tradeOrderSpec struct {
	Price       string `json:"price"`
	TimeInForce string `json:"timeInForce"`
}

// UpdateTradeStop updates the stop-loss (and optionally take-profit) on an
// open trade. Pass 0 to leave a level unchanged. Pass a negative value to
// cancel an existing order.
func (c *Client) UpdateTradeStop(ctx context.Context, accountID, tradeID string, stopPrice, takePrice float64) error {
	req := updateTradeOrdersReq{}

	if stopPrice > 0 {
		req.StopLoss = &tradeOrderSpec{
			Price:       strconv.FormatFloat(stopPrice, 'f', 5, 64),
			TimeInForce: "GTC",
		}
	} else if stopPrice < 0 {
		req.StopLoss = &tradeOrderSpec{TimeInForce: "GTC"} // cancel: omit price
	}

	if takePrice > 0 {
		req.TakeProfit = &tradeOrderSpec{
			Price:       strconv.FormatFloat(takePrice, 'f', 5, 64),
			TimeInForce: "GTC",
		}
	} else if takePrice < 0 {
		req.TakeProfit = &tradeOrderSpec{TimeInForce: "GTC"} // cancel
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	u.Path = fmt.Sprintf("/v3/accounts/%s/trades/%s/orders", accountID, tradeID)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("oanda: update trade orders http %d: %s", resp.StatusCode, trimForErr(string(b)))
	}
	return nil
}
