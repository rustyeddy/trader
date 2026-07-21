package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// PlaceMarketOrderRequest is the typed input for risk-sized market orders.
// Either StopPips or StopPrice must be set; if both are set, StopPrice wins.
type PlaceMarketOrderRequest struct {
	Instrument string     // OANDA format, e.g. "USD_JPY"
	Side       string     // "long" or "short"
	RiskPct    types.Rate // fraction of account NAV to risk (0.01×RateScale = 1%)
	StopPips   float64    // stop distance in pips (mutually exclusive with StopPrice)
	StopPrice  float64    // explicit stop price (overrides StopPips)
	// Units may override risk-based sizing. When 0, units are computed
	// from RiskPct + stop distance.
	Units int64
	// MaxUnits caps the absolute unit count after risk-based sizing. 0 = no cap.
	MaxUnits int64
	// MaxPositionUSD caps the notional value of the position in account currency.
	// Units are reduced so that (|units| * entryPrice) ≤ MaxPositionUSD. 0 = no cap.
	MaxPositionUSD float64
	// Confirm must be true to actually submit the order. False returns the
	// proposed order in PlaceMarketOrderResult.Proposal without sending.
	Confirm bool
}

// PlaceMarketOrderResult describes the proposed (or filled) order. When
// Confirm=false, Filled is nil and Proposal is populated. When Confirm=true
// and the order succeeds, Filled is populated.
type PlaceMarketOrderResult struct {
	Proposal OrderProposal
	Filled   *oanda.OrderResult // nil if Confirm was false
}

// OrderProposal is what the user is about to submit (or just submitted).
// Surfaces the sizing math so callers can render a confirmation view.
type OrderProposal struct {
	Instrument string
	Side       string  // "long" or "short"
	Units      int64   // signed (long = positive, short = negative)
	EntryPrice float64 // expected fill price (ask for long, bid for short)
	StopPrice  float64
	RiskAmount float64 // USD risked
	AccountNAV float64
}

// PlaceMarketOrder handles the full risk-sized order workflow:
//  1. Resolve account (if needed)
//  2. Fetch live price + account summary
//  3. Compute stop, size, risk
//  4. Build proposal
//  5. If Confirm: submit to OANDA and return the fill result
//  6. Otherwise: return the proposal for caller to confirm with the user
func (a *Account) PlaceMarketOrder(ctx context.Context, req PlaceMarketOrderRequest) (*PlaceMarketOrderResult, error) {
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side != "long" && side != "short" {
		return nil, fmt.Errorf("side must be 'long' or 'short', got %q", req.Side)
	}

	prices, err := a.svc.OANDA.GetPricing(ctx, a.ID, req.Instrument)
	if err != nil {
		return nil, fmt.Errorf("get pricing: %w", err)
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("no price returned for %s", req.Instrument)
	}
	px := prices[0]
	// Convert wire-format floats to fixed-point at the API boundary.
	entryPrice := types.PriceFromFloat(px.Ask)
	if side == "short" {
		entryPrice = types.PriceFromFloat(px.Bid)
	}

	var summary *oanda.AccountSummary
	if snap := a.getSnapshot(); snap != nil {
		summary = snap.Summary()
	} else {
		s, err := a.broker().GetAccountSummary(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("get account: %w", err)
		}
		summary = s
	}
	equity := summary.NAV
	if equity <= 0 {
		return nil, fmt.Errorf("account equity is zero or unavailable")
	}
	currency := summary.Currency
	if currency == "" {
		currency = "USD"
	}

	// Stop price as fixed-point.
	var stopPrice types.Price
	switch {
	case req.StopPrice > 0:
		stopPrice = types.PriceFromFloat(req.StopPrice)
	case req.StopPips > 0:
		inst := market.GetInstrument(market.NormalizeInstrument(req.Instrument))
		pips := types.PipsFromFloat(req.StopPips)
		if inst != nil {
			if side == "long" {
				stopPrice = inst.SubPips(entryPrice, pips)
			} else {
				stopPrice = inst.AddPips(entryPrice, pips)
			}
		} else {
			// Unknown instrument — fall back to price-unit approximation.
			delta := types.PriceFromFloat(req.StopPips * 0.0001)
			if strings.Contains(strings.ToUpper(req.Instrument), "JPY") {
				delta = types.PriceFromFloat(req.StopPips * 0.01)
			}
			if side == "long" {
				stopPrice = entryPrice - delta
			} else {
				stopPrice = entryPrice + delta
			}
		}
	default:
		return nil, fmt.Errorf("either StopPrice or StopPips is required")
	}

	stopDist := entryPrice - stopPrice
	if stopDist < 0 {
		stopDist = -stopDist
	}
	if stopDist == 0 {
		return nil, fmt.Errorf("stop distance is zero — check stop price")
	}

	sideEnum := types.Long
	if side == "short" {
		sideEnum = types.Short
	}
	normInst := market.NormalizeInstrument(req.Instrument)

	var units int64
	var riskAmount float64
	if req.Units != 0 {
		units = req.Units
	} else {
		inputs := account.SizingInputs{
			Equity:       types.MoneyFromFloat(equity),
			MarginUsed:   types.MoneyFromFloat(summary.MarginUsed),
			FreeMargin:   types.MoneyFromFloat(summary.MarginAvail),
			RiskFraction: req.RiskPct,
			Currency:     currency,
		}
		openReq := &account.OpenRequest{
			Request: account.Request{
				TradeCommon: &account.TradeCommon{
					Instrument: normInst,
					Side:       sideEnum,
					Stop:       stopPrice,
				},
				Price: entryPrice,
			},
		}
		if err := account.SizePosition(inputs, openReq); err != nil {
			return nil, fmt.Errorf("size position: %w", err)
		}
		units = int64(openReq.Units)
		riskAmount = float64(inputs.RiskFraction.Float64()) * equity
	}

	// Apply caps before signing the units (caps work on absolute values).
	if req.MaxUnits > 0 && units > req.MaxUnits {
		units = req.MaxUnits
	}
	if req.MaxPositionUSD > 0 && entryPrice > 0 {
		maxByNotional := int64(math.Floor(req.MaxPositionUSD / entryPrice.Float64()))
		if maxByNotional < 1 {
			maxByNotional = 1
		}
		if units > maxByNotional {
			units = maxByNotional
		}
	}

	if side == "short" {
		units = -units
	}

	proposal := OrderProposal{
		Instrument: req.Instrument,
		Side:       side,
		Units:      units,
		EntryPrice: entryPrice.Float64(),
		StopPrice:  stopPrice.Float64(),
		RiskAmount: riskAmount,
		AccountNAV: equity,
	}
	result := &PlaceMarketOrderResult{Proposal: proposal}

	if !req.Confirm {
		return result, nil
	}

	fill, err := a.broker().SubmitMarketOrder(ctx, a.ID, req.Instrument, units, stopPrice.Float64())
	if err != nil {
		return result, fmt.Errorf("submit order: %w", err)
	}
	result.Filled = fill
	a.svc.Log.Info("service: market order filled",
		"order_id", fill.OrderID, "trade_id", fill.TradeID,
		"instrument", fill.Instrument, "units", fill.Units, "price", fill.Price,
	)
	return result, nil
}

// CloseTrade closes a trade by ID. Units=0 means full close; >0 is partial.
func (a *Account) CloseTrade(ctx context.Context, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	res, err := a.broker().CloseTrade(ctx, a.ID, tradeID, units)
	if err != nil {
		return nil, fmt.Errorf("close trade %s: %w", tradeID, err)
	}
	a.svc.Log.Info("service: trade closed",
		"trade_id", res.TradeID, "units", res.Units, "price", res.Price,
	)
	return res, nil
}

// UpdateTradeStop updates the broker-side stop and/or take-profit on an
// open trade. stopPx/takePx use the same convention as
// oanda.Client.UpdateTradeStop: >0 sets, 0 leaves unchanged, <0 cancels.
func (a *Account) UpdateTradeStop(ctx context.Context, tradeID string, stopPx, takePx float64) error {
	if err := a.broker().UpdateTradeStop(ctx, a.ID, tradeID, stopPx, takePx); err != nil {
		return fmt.Errorf("update stop %s: %w", tradeID, err)
	}
	return nil
}

// ListOpenTrades returns the open positions on the account.
// When the account snapshot is running it reads from the local cache;
// otherwise it falls back to a direct OANDA REST call.
func (a *Account) ListOpenTrades(ctx context.Context) ([]oanda.OpenTrade, error) {
	if snap := a.getSnapshot(); snap != nil {
		return snap.OpenTrades(), nil
	}
	trades, err := a.broker().GetOpenTrades(ctx, a.ID)
	if err != nil {
		return nil, fmt.Errorf("get open trades: %w", err)
	}
	return trades, nil
}

// ── default-account back-compat wrappers ─────────────────────────────────────

// PlaceMarketOrder runs the risk-sized order workflow on the default account.
func (s *Service) PlaceMarketOrder(ctx context.Context, req PlaceMarketOrderRequest) (*PlaceMarketOrderResult, error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.PlaceMarketOrder(ctx, req)
}

// CloseTrade closes a trade on the default account.
func (s *Service) CloseTrade(ctx context.Context, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.CloseTrade(ctx, tradeID, units)
}

// UpdateTradeStop updates a stop/take-profit on the default account.
func (s *Service) UpdateTradeStop(ctx context.Context, tradeID string, stopPx, takePx float64) error {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return err
	}
	return acc.UpdateTradeStop(ctx, tradeID, stopPx, takePx)
}

// ListOpenTrades returns the open positions on the first/default account (read).
func (s *Service) ListOpenTrades(ctx context.Context) ([]oanda.OpenTrade, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.ListOpenTrades(ctx)
}
