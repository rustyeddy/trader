package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// AccountDetails is the full account snapshot from GET /v3/accounts/{id}.
// Unlike AccountSummary (which omits positions), this includes open trades and
// the lastTransactionID needed to seed the incremental changes poll loop.
type AccountDetails struct {
	AccountSummary
	OpenTrades        []OpenTrade
	LastTransactionID int64
}

// AccountChangesResult holds the parsed response from
// GET /v3/accounts/{id}/changes?sinceTransactionID=N.
// Callers should apply structural changes (TradesOpened / TradesClosed /
// TradesReduced) first, then overwrite the price-dependent state fields.
type AccountChangesResult struct {
	// Structural mutations — apply as add / remove / update.
	TradesOpened  []OpenTrade
	TradesClosed  []string         // IDs of fully-closed trades
	TradesReduced map[string]int64 // trade ID → new currentUnits after partial close

	// Price-dependent state — replace on every poll.
	NAV          float64
	UnrealizedPL float64
	MarginUsed   float64
	MarginAvail  float64
	// TradeState maps trade ID → current unrealizedPL from the state array.
	TradeState map[string]float64

	// BalanceAfterFill is the account balance after the most recent ORDER_FILL
	// transaction in this changes window, or 0 if no fill occurred.
	BalanceAfterFill float64

	LastTransactionID int64
}

// ─── wire types ──────────────────────────────────────────────────────────────

// tradeDetailWire is the on-wire trade shape shared by the full account
// details response (account.trades) and the changes.tradesOpened array.
type tradeDetailWire struct {
	ID            string `json:"id"`
	Instrument    string `json:"instrument"`
	Price         string `json:"price"`
	CurrentUnits  string `json:"currentUnits"`
	UnrealizedPL  string `json:"unrealizedPL"`
	OpenTime      string `json:"openTime"`
	StopLossOrder *struct {
		Price string `json:"price"`
	} `json:"stopLossOrder"`
	TakeProfitOrder *struct {
		Price string `json:"price"`
	} `json:"takeProfitOrder"`
}

type accountDetailsWire struct {
	Account struct {
		ID           string            `json:"id"`
		Alias        string            `json:"alias"`
		Currency     string            `json:"currency"`
		Balance      string            `json:"balance"`
		NAV          string            `json:"NAV"`
		UnrealizedPL string            `json:"unrealizedPL"`
		MarginUsed   string            `json:"marginUsed"`
		MarginAvail  string            `json:"marginAvailable"`
		LastTxID     string            `json:"lastTransactionID"`
		Trades       []tradeDetailWire `json:"trades"`
	} `json:"account"`
	LastTransactionID string `json:"lastTransactionID"`
}

type accountChangesWire struct {
	Changes struct {
		TradesOpened []tradeDetailWire `json:"tradesOpened"`
		TradesClosed []struct {
			ID string `json:"id"`
		} `json:"tradesClosed"`
		TradesReduced []struct {
			ID           string `json:"id"`
			CurrentUnits string `json:"currentUnits"`
		} `json:"tradesReduced"`
		Transactions []struct {
			Type           string `json:"type"`
			AccountBalance string `json:"accountBalance"`
		} `json:"transactions"`
	} `json:"changes"`
	State struct {
		NAV          string `json:"NAV"`
		UnrealizedPL string `json:"unrealizedPL"`
		MarginUsed   string `json:"marginUsed"`
		MarginAvail  string `json:"marginAvailable"`
		Trades       []struct {
			ID           string `json:"id"`
			UnrealizedPL string `json:"unrealizedPL"`
		} `json:"trades"`
	} `json:"state"`
	LastTransactionID string `json:"lastTransactionID"`
}

func parseTradeDetailWire(t tradeDetailWire) (OpenTrade, error) {
	ot := OpenTrade{
		ID:         t.ID,
		Instrument: t.Instrument,
	}
	var err error
	ot.EntryPrice, err = parseFloatField("trade price", t.Price)
	if err != nil {
		return OpenTrade{}, err
	}
	ot.Units, err = parseIntField("trade currentUnits", t.CurrentUnits)
	if err != nil {
		return OpenTrade{}, err
	}
	ot.UnrealizedPL, err = parseOptionalFloatField("trade unrealizedPL", t.UnrealizedPL)
	if err != nil {
		return OpenTrade{}, err
	}
	if t.OpenTime != "" {
		ot.OpenTime, err = parseTimeField("trade openTime", t.OpenTime)
		if err != nil {
			return OpenTrade{}, err
		}
	}
	if t.StopLossOrder != nil && t.StopLossOrder.Price != "" {
		ot.StopLoss, err = parseFloatField("trade stopLoss", t.StopLossOrder.Price)
		if err != nil {
			return OpenTrade{}, err
		}
	}
	if t.TakeProfitOrder != nil && t.TakeProfitOrder.Price != "" {
		ot.TakeProfit, err = parseFloatField("trade takeProfit", t.TakeProfitOrder.Price)
		if err != nil {
			return OpenTrade{}, err
		}
	}
	return ot, nil
}

// GetAccountDetails fetches the full account snapshot from OANDA including
// open trades and lastTransactionID. Use this once at startup to seed the
// incremental GetAccountChanges poll loop.
func (c *Client) GetAccountDetails(ctx context.Context, accountID string) (*AccountDetails, error) {
	body, err := c.Get(ctx, fmt.Sprintf("/v3/accounts/%s", accountID), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var wire accountDetailsWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("oanda: parse account details: %w", err)
	}

	a := wire.Account
	balance, err := parseOptionalFloatField("balance", a.Balance)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	nav, err := parseOptionalFloatField("NAV", a.NAV)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	unrealizedPL, err := parseOptionalFloatField("unrealizedPL", a.UnrealizedPL)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	marginUsed, err := parseOptionalFloatField("marginUsed", a.MarginUsed)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	marginAvail, err := parseOptionalFloatField("marginAvailable", a.MarginAvail)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}

	var lastTxID int64
	if a.LastTxID != "" {
		lastTxID, err = parseIntField("lastTransactionID", a.LastTxID)
		if err != nil {
			return nil, fmt.Errorf("oanda: %w", err)
		}
	}

	trades := make([]OpenTrade, 0, len(a.Trades))
	for _, t := range a.Trades {
		ot, err := parseTradeDetailWire(t)
		if err != nil {
			return nil, fmt.Errorf("oanda: parse trade: %w", err)
		}
		trades = append(trades, ot)
	}

	return &AccountDetails{
		AccountSummary: AccountSummary{
			ID:           a.ID,
			Alias:        a.Alias,
			Currency:     a.Currency,
			Balance:      balance,
			NAV:          nav,
			UnrealizedPL: unrealizedPL,
			MarginUsed:   marginUsed,
			MarginAvail:  marginAvail,
		},
		OpenTrades:        trades,
		LastTransactionID: lastTxID,
	}, nil
}

// GetAccountChanges polls for structural and price-dependent account changes
// since sinceID. Returns the changes plus the new lastTransactionID to use on
// the next call. The caller is responsible for applying changes in the correct
// order: structural first (TradesOpened / TradesClosed / TradesReduced), then
// overwrite price-dependent state fields.
func (c *Client) GetAccountChanges(ctx context.Context, accountID string, sinceID int64) (*AccountChangesResult, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("oanda: missing token")
	}
	body, err := c.Get(ctx,
		fmt.Sprintf("/v3/accounts/%s/changes", accountID),
		map[string]string{"sinceTransactionID": strconv.FormatInt(sinceID, 10)},
	)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var wire accountChangesWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("oanda: parse account changes: %w", err)
	}

	result := &AccountChangesResult{
		TradesReduced: make(map[string]int64),
		TradeState:    make(map[string]float64),
	}

	// Parse structural changes.
	for _, t := range wire.Changes.TradesOpened {
		ot, err := parseTradeDetailWire(t)
		if err != nil {
			return nil, fmt.Errorf("oanda: parse opened trade: %w", err)
		}
		result.TradesOpened = append(result.TradesOpened, ot)
	}
	for _, t := range wire.Changes.TradesClosed {
		result.TradesClosed = append(result.TradesClosed, t.ID)
	}
	for _, t := range wire.Changes.TradesReduced {
		units, err := parseIntField("reduced trade currentUnits", t.CurrentUnits)
		if err != nil {
			return nil, fmt.Errorf("oanda: %w", err)
		}
		result.TradesReduced[t.ID] = units
	}

	// Extract latest balance from ORDER_FILL transactions.
	for _, tx := range wire.Changes.Transactions {
		if tx.Type == "ORDER_FILL" && tx.AccountBalance != "" {
			bal, err := parseFloatField("transaction accountBalance", tx.AccountBalance)
			if err == nil {
				result.BalanceAfterFill = bal
			}
		}
	}

	// Parse price-dependent state.
	s := wire.State
	result.NAV, err = parseOptionalFloatField("state NAV", s.NAV)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	result.UnrealizedPL, err = parseOptionalFloatField("state unrealizedPL", s.UnrealizedPL)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	result.MarginUsed, err = parseOptionalFloatField("state marginUsed", s.MarginUsed)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	result.MarginAvail, err = parseOptionalFloatField("state marginAvailable", s.MarginAvail)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	for _, t := range s.Trades {
		upl, err := parseOptionalFloatField("state trade unrealizedPL", t.UnrealizedPL)
		if err == nil {
			result.TradeState[t.ID] = upl
		}
	}

	if wire.LastTransactionID != "" {
		result.LastTransactionID, err = parseIntField("lastTransactionID", wire.LastTransactionID)
		if err != nil {
			return nil, fmt.Errorf("oanda: %w", err)
		}
	}

	return result, nil
}
