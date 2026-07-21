package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// Sim is a simulated Broker (brokers.Broker): it fills orders against its
// own tracked prices (via UpdatePrice, or synthesized from candles via
// TickFromCandle) instead of a real network round-trip, using the same
// account.Ledger-adjacent bookkeeping (Account.AddLot/CloseLot) a real
// broker's fills would eventually feed. Wraps exactly one account — the
// accountID parameter on Broker methods is accepted for interface
// compliance but not used to look up among multiple accounts.
type Sim struct {
	account *account.Account
	journal journal.Journal
	prices  map[string]market.Tick

	// Slippage is added beyond the tracked bid/ask spread on every fill,
	// mirroring backtest/execute.go's slippage parameter. Zero by default
	// (no extra adverse movement beyond the quoted spread).
	Slippage types.Price
}

func NewSimBroker(acct *account.Account, j journal.Journal) *Sim {
	if acct == nil {
		acct = account.NewAccount("sim", 0)
	}
	if acct.Lots.All() == nil {
		acct.Lots = account.LotBook{}
	}
	return &Sim{
		account: acct,
		journal: j,
		prices:  make(map[string]market.Tick),
	}
}

func (e *Sim) UpdatePrice(tick market.Tick) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("sim broker account is nil")
	}
	inst := market.NormalizeInstrument(tick.Instrument)
	if inst == "" {
		return fmt.Errorf("blank instrument")
	}
	tick.Instrument = inst
	if err := tick.Validate(); err != nil {
		return err
	}
	e.prices[inst] = tick

	marks := make(map[string]types.Price, len(e.prices))
	for instrument, px := range e.prices {
		marks[instrument] = px.Mid()
	}
	return e.account.ResolveWithMarks(marks)
}

func (e *Sim) CloseAll(ctx context.Context, reason string) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("sim broker account is nil")
	}

	var lots []*account.Lot
	_ = e.account.Lots.Range(func(lot *account.Lot) error {
		lots = append(lots, lot)
		return nil
	})

	for _, lot := range lots {
		px, ok := e.prices[lot.Instrument]
		if !ok {
			return fmt.Errorf("no market price for %s", lot.Instrument)
		}
		exitPrice := px.Mid()
		exitTime := types.FromTime(time.Now().UTC())
		trade := &account.Trade{
			TradeCommon: lot.TradeCommon.Clone(),
			EntryPrice:  lot.EntryPrice,
			EntryTime:   lot.EntryTime,
			ExitPrice:   exitPrice,
			ExitTime:    exitTime,
		}
		if err := e.account.CloseLot(lot, trade); err != nil {
			return err
		}
		if e.journal != nil {
			_ = e.journal.RecordTrade(journal.TradeRecord{
				TradeID:    trade.ID,
				Instrument: trade.Instrument,
				Units:      trade.Units,
				EntryPrice: lot.EntryPrice,
				ExitPrice:  trade.ExitPrice,
				OpenTime:   lot.EntryTime,
				CloseTime:  trade.ExitTime,
				RealizedPL: trade.PNL,
				Reason:     reason,
			})
		}
	}

	if e.journal != nil {
		_ = e.journal.RecordEquity(journal.EquitySnapshot{
			Timestamp:   types.FromTime(time.Now().UTC()),
			Balance:     e.account.Balance,
			Equity:      e.account.Equity,
			MarginUsed:  e.account.MarginUsed,
			FreeMargin:  e.account.FreeMargin,
			MarginLevel: e.account.MarginLevel,
		})
	}

	return nil
}

func (e *Sim) GetAccount(context.Context) (*account.Account, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	return e.account, nil
}

// ── brokers.Broker: order execution ─────────────────────────────────────────

// SubmitMarketOrder fills immediately against the tracked price for
// instrument (positive units = long, filled at ask; negative = short,
// filled at bid), plus Slippage, and opens a Lot via Account.AddLot — the
// same bookkeeping path account.Ledger.SubmitOpen uses.
func (e *Sim) SubmitMarketOrder(ctx context.Context, accountID, instrument string, units int64, stopPrice float64) (*oanda.OrderResult, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	if units == 0 {
		return nil, fmt.Errorf("sim: units must be non-zero")
	}
	inst := market.NormalizeInstrument(instrument)
	px, ok := e.prices[inst]
	if !ok {
		return nil, fmt.Errorf("sim: no market price for %s", inst)
	}

	isBuy := units > 0
	side := types.Short
	fillPrice := px.Bid
	if isBuy {
		side = types.Long
		fillPrice = px.Ask
	}
	fillPrice += account.FillAdjust(isBuy, 0, e.Slippage)

	absUnits := units
	if absUnits < 0 {
		absUnits = -absUnits
	}

	lot := &account.Lot{
		TradeCommon: &account.TradeCommon{
			ID:         idgen.NewULID(),
			Instrument: inst,
			Side:       side,
			Units:      types.Units(absUnits),
			Stop:       types.PriceFromFloat(stopPrice),
		},
		EntryPrice:     fillPrice,
		EntryTime:      px.Timestamp,
		OriginalUnits:  types.Units(absUnits),
		RemainingUnits: types.Units(absUnits),
		State:          account.LotOpen,
	}

	if err := e.account.AddLot(lot); err != nil {
		return nil, fmt.Errorf("sim: open lot: %w", err)
	}

	return &oanda.OrderResult{
		OrderID:    lot.ID,
		TradeID:    lot.ID,
		Instrument: inst,
		Units:      units,
		Price:      fillPrice.Float64(),
	}, nil
}

// CloseTrade closes the lot identified by tradeID at the current tracked
// price, plus Slippage. units is accepted for Broker interface parity with
// OANDA's partial-close semantics, but a full close is all this chunk
// implements — partial closes are a later chunk's concern, once
// account.Ledger's own partial-close handling (if any) needs mirroring.
func (e *Sim) CloseTrade(ctx context.Context, accountID, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	lot := e.account.Lots.Get(tradeID)
	if lot == nil {
		return nil, fmt.Errorf("sim: no open trade %s", tradeID)
	}
	px, ok := e.prices[lot.Instrument]
	if !ok {
		return nil, fmt.Errorf("sim: no market price for %s", lot.Instrument)
	}

	// Closing a long means selling (fills at bid); closing a short means
	// buying to cover (fills at ask) — the mirror image of opening.
	isBuy := lot.Side == types.Short
	exitPrice := px.Bid
	if isBuy {
		exitPrice = px.Ask
	}
	exitPrice += account.FillAdjust(isBuy, 0, e.Slippage)

	trade := &account.Trade{
		TradeCommon: lot.TradeCommon.Clone(),
		EntryPrice:  lot.EntryPrice,
		EntryTime:   lot.EntryTime,
		ExitPrice:   exitPrice,
		ExitTime:    px.Timestamp,
	}
	if err := e.account.CloseLot(lot, trade); err != nil {
		return nil, fmt.Errorf("sim: close lot: %w", err)
	}

	if e.journal != nil {
		_ = e.journal.RecordTrade(journal.TradeRecord{
			TradeID:    trade.ID,
			Instrument: trade.Instrument,
			Units:      trade.Units,
			EntryPrice: lot.EntryPrice,
			ExitPrice:  trade.ExitPrice,
			OpenTime:   lot.EntryTime,
			CloseTime:  trade.ExitTime,
			RealizedPL: trade.PNL,
			Reason:     "sim close",
		})
	}

	return &oanda.CloseTradeResult{
		TradeID: trade.ID,
		Units:   int64(trade.Units),
		Price:   exitPrice.Float64(),
	}, nil
}

// UpdateTradeStop sets the stop/take on an open lot. Convention matches
// oanda.Client.UpdateTradeStop: >0 sets, 0 leaves unchanged, <0 cancels.
func (e *Sim) UpdateTradeStop(ctx context.Context, accountID, tradeID string, stopPrice, takePrice float64) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("sim broker account is nil")
	}
	// LotBook.Get returns a clone (mutations wouldn't persist) — Range
	// yields the live stored pointers, which this needs to actually
	// update the book.
	found := false
	_ = e.account.Lots.Range(func(lot *account.Lot) error {
		if lot.ID != tradeID {
			return nil
		}
		found = true
		switch {
		case stopPrice > 0:
			lot.Stop = types.PriceFromFloat(stopPrice)
		case stopPrice < 0:
			lot.Stop = 0
		}
		switch {
		case takePrice > 0:
			lot.Take = types.PriceFromFloat(takePrice)
		case takePrice < 0:
			lot.Take = 0
		}
		return nil
	})
	if !found {
		return fmt.Errorf("sim: no open trade %s", tradeID)
	}
	return nil
}

// ── brokers.Broker: account state ───────────────────────────────────────────

// GetOpenTrades returns every open lot as an oanda.OpenTrade.
func (e *Sim) GetOpenTrades(ctx context.Context, accountID string) ([]oanda.OpenTrade, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	lots := e.account.Lots.Slice()
	out := make([]oanda.OpenTrade, 0, len(lots))
	for _, lot := range lots {
		units := int64(lot.RemainingUnits)
		if lot.Side == types.Short {
			units = -units
		}
		out = append(out, oanda.OpenTrade{
			ID:           lot.ID,
			Instrument:   lot.Instrument,
			EntryPrice:   lot.EntryPrice.Float64(),
			Units:        units,
			UnrealizedPL: 0, // ResolveWithMarks tracks this at the Account level, not per-lot here
			StopLoss:     lot.Stop.Float64(),
			TakeProfit:   lot.Take.Float64(),
			OpenTime:     lot.EntryTime.Time(),
		})
	}
	return out, nil
}

// GetAccountSummary maps the simulated Account's ledger state onto OANDA's
// account-summary shape.
func (e *Sim) GetAccountSummary(ctx context.Context, accountID string) (*oanda.AccountSummary, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	a := e.account
	return &oanda.AccountSummary{
		ID:           a.ID,
		Currency:     a.Currency,
		Balance:      a.Balance.Float64(),
		NAV:          a.Equity.Float64(),
		UnrealizedPL: (a.Equity - a.Balance).Float64(),
		MarginUsed:   a.MarginUsed.Float64(),
		MarginAvail:  a.FreeMargin.Float64(),
	}, nil
}

// GetAccountDetails is GetAccountSummary plus the open-trades list, mirroring
// oanda.AccountDetails' shape (summary + positions in one call).
func (e *Sim) GetAccountDetails(ctx context.Context, accountID string) (*oanda.AccountDetails, error) {
	summary, err := e.GetAccountSummary(ctx, accountID)
	if err != nil {
		return nil, err
	}
	trades, err := e.GetOpenTrades(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &oanda.AccountDetails{
		AccountSummary: *summary,
		OpenTrades:     trades,
	}, nil
}
