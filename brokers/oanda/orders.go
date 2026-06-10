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
)

// OrderResult is the response from a successfully submitted market order.
type OrderResult struct {
	OrderID    string
	TradeID    string
	Instrument string
	Units      int64
	Price      float64 // fill price
}

type marketOrderBody struct {
	Order marketOrderSpec `json:"order"`
}

type marketOrderSpec struct {
	Type           string        `json:"type"`
	Instrument     string        `json:"instrument"`
	Units          string        `json:"units"`
	StopLossOnFill *stopLossSpec `json:"stopLossOnFill,omitempty"`
	TimeInForce    string        `json:"timeInForce"`
}

type stopLossSpec struct {
	Price string `json:"price"`
}

type orderResp struct {
	OrderFillTransaction struct {
		ID          string `json:"id"`
		TradeOpened struct {
			TradeID string `json:"tradeID"`
		} `json:"tradeOpened"`
		// OANDA netting: when the order nets against an existing position the
		// trade ID appears in tradesClosed or tradeReduced, not tradeOpened.
		TradesClosed []struct {
			TradeID string `json:"tradeID"`
		} `json:"tradesClosed"`
		TradeReduced struct {
			TradeID string `json:"tradeID"`
		} `json:"tradeReduced"`
		Instrument string `json:"instrument"`
		Units      string `json:"units"`
		Price      string `json:"price"`
	} `json:"orderFillTransaction"`
	// OANDA returns these instead of orderFillTransaction when a FOK order
	// cannot be filled or is rejected before reaching the market.
	OrderCancelTransaction struct {
		Reason string `json:"reason"`
	} `json:"orderCancelTransaction"`
	OrderRejectTransaction struct {
		Reason       string `json:"reason"`
		RejectReason string `json:"rejectReason"`
	} `json:"orderRejectTransaction"`
	RelatedTransactionIDs []string `json:"relatedTransactionIDs"`
}

// SubmitMarketOrder places a market order on OANDA.
// units > 0 = long, units < 0 = short.
// stopPrice = 0 means no stop loss attached.
func (c *Client) SubmitMarketOrder(ctx context.Context, accountID, instrument string, units int64, stopPrice float64) (*OrderResult, error) {
	if units == 0 {
		return nil, fmt.Errorf("oanda: units must be non-zero")
	}

	spec := marketOrderSpec{
		Type:        "MARKET",
		Instrument:  instrument,
		Units:       strconv.FormatInt(units, 10),
		TimeInForce: "FOK", // Fill or Kill
	}
	if stopPrice > 0 {
		spec.StopLossOnFill = &stopLossSpec{
			Price: strconv.FormatFloat(stopPrice, 'f', 5, 64),
		}
	}

	body, err := json.Marshal(marketOrderBody{Order: spec})
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = fmt.Sprintf("/v3/accounts/%s/orders", accountID)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
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

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("oanda: submit order http %d: %s", resp.StatusCode, trimForErr(string(respData)))
	}

	var or orderResp
	if err := json.Unmarshal(respData, &or); err != nil {
		return nil, fmt.Errorf("oanda: parse order response: %w", err)
	}

	// OANDA returns 201 even when a FOK order is cancelled or rejected;
	// detect these by the absence of orderFillTransaction.id.
	if or.OrderFillTransaction.ID == "" {
		if r := or.OrderRejectTransaction.RejectReason; r != "" {
			return nil, fmt.Errorf("oanda: order rejected: %s", r)
		}
		if r := or.OrderCancelTransaction.Reason; r != "" {
			return nil, fmt.Errorf("oanda: order cancelled: %s", r)
		}
		return nil, fmt.Errorf("oanda: order not filled (no fill transaction in response)")
	}

	fillUnits, err := parseIntField("fill units", or.OrderFillTransaction.Units)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	fillPrice, err := parseFloatField("fill price", or.OrderFillTransaction.Price)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}

	tradeID := or.OrderFillTransaction.TradeOpened.TradeID
	if tradeID == "" && len(or.OrderFillTransaction.TradesClosed) > 0 {
		tradeID = or.OrderFillTransaction.TradesClosed[0].TradeID
	}
	if tradeID == "" {
		tradeID = or.OrderFillTransaction.TradeReduced.TradeID
	}

	return &OrderResult{
		OrderID:    or.OrderFillTransaction.ID,
		TradeID:    tradeID,
		Instrument: or.OrderFillTransaction.Instrument,
		Units:      fillUnits,
		Price:      fillPrice,
	}, nil
}
