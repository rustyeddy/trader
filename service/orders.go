package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
)

// PlaceMarketOrderRequest is the typed input for risk-sized market orders.
// Either StopPips or StopPrice must be set; if both are set, StopPrice wins.
type PlaceMarketOrderRequest struct {
	Instrument string      // OANDA format, e.g. "USD_JPY"
	Side       string      // "long" or "short"
	RiskPct    market.Rate // fraction of account NAV to risk (0.01×RateScale = 1%)
	StopPips   float64     // stop distance in pips (mutually exclusive with StopPrice)
	StopPrice  float64     // explicit stop price (overrides StopPips)
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
	entry := px.Ask
	if side == "short" {
		entry = px.Bid
	}

	var equity float64
	if snap := a.getSnapshot(); snap != nil {
		equity = snap.NAV()
	} else {
		acct, err := a.svc.OANDA.GetAccountSummary(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("get account: %w", err)
		}
		equity = acct.NAV
	}
	if equity <= 0 {
		return nil, fmt.Errorf("account equity is zero or unavailable")
	}

	// Stop price.
	var stop float64
	switch {
	case req.StopPrice > 0:
		stop = req.StopPrice
	case req.StopPips > 0:
		pipSize := 0.0001
		if strings.Contains(req.Instrument, "JPY") {
			pipSize = 0.01
		}
		dist := req.StopPips * pipSize
		if side == "long" {
			stop = entry - dist
		} else {
			stop = entry + dist
		}
	default:
		return nil, fmt.Errorf("either StopPrice or StopPips is required")
	}

	stopDist := math.Abs(entry - stop)
	if stopDist == 0 {
		return nil, fmt.Errorf("stop distance is zero — check stop price")
	}

	// Convert stop distance from quote currency to account currency (USD).
	// stopDist is in quote-currency units (e.g. JPY for USD_JPY, CHF for USD_CHF).
	// Dividing by the approximate USD-per-quote-unit gives USD risk per unit.
	// For USD-quoted pairs (GBP_USD) the rate is 1.0 — no adjustment needed.
	stopDistUSD := stopDist * quoteToUSDRate(req.Instrument)

	// Sizing.
	riskAmount := equity * req.RiskPct.Float64()
	units := req.Units
	if units == 0 {
		units = int64(math.Round(riskAmount / stopDistUSD))
		if units < 1 {
			units = 1
		}
	}

	// Apply caps before signing the units (caps work on absolute values).
	if req.MaxUnits > 0 && units > req.MaxUnits {
		units = req.MaxUnits
	}
	if req.MaxPositionUSD > 0 && entry > 0 {
		maxByNotional := int64(math.Floor(req.MaxPositionUSD / entry))
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
		EntryPrice: entry,
		StopPrice:  stop,
		RiskAmount: riskAmount,
		AccountNAV: equity,
	}
	result := &PlaceMarketOrderResult{Proposal: proposal}

	if !req.Confirm {
		return result, nil
	}

	fill, err := a.svc.OANDA.SubmitMarketOrder(ctx, a.ID, req.Instrument, units, stop)
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

// quoteToUSDRate returns an approximate multiplier to convert a price distance
// in the instrument's quote currency to USD. For USD-quoted pairs (GBP_USD,
// EUR_USD) the rate is 1.0. For JPY-quoted pairs (USD_JPY) it is ~0.0067
// (1/150). Uses the same static table as the backtest P/L conversion.
// Accuracy is ±30% over long periods; sufficient for position sizing purposes.
func quoteToUSDRate(instrument string) float64 {
	inst := market.GetInstrument(market.NormalizeInstrument(instrument))
	if inst == nil {
		return 1.0 // unknown — no adjustment
	}
	if inst.QuoteCurrency == "USD" {
		return 1.0
	}
	if r, ok := market.ApproximateUSDPerUnit(inst.QuoteCurrency); ok {
		return r
	}
	return 1.0
}

// CloseTrade closes a trade by ID. Units=0 means full close; >0 is partial.
func (a *Account) CloseTrade(ctx context.Context, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	res, err := a.svc.OANDA.CloseTrade(ctx, a.ID, tradeID, units)
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
	if err := a.svc.OANDA.UpdateTradeStop(ctx, a.ID, tradeID, stopPx, takePx); err != nil {
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
	trades, err := a.svc.OANDA.GetOpenTrades(ctx, a.ID)
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
