package sim

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
)

type Engine struct {
	mu       sync.Mutex
	acct     broker.Account
	prices   *PriceStore
	trades   map[string]*Trade
	nextID   int
	journal  journal.Journal
	listener tradeClosedListener // optional callback for auto-closed trades
}

// tradeClosedListener is notified when the engine auto-closes a trade.
// This is an internal interface; external code should use strategies.TradeClosedListener.
type tradeClosedListener interface {
	OnTradeClosed(tradeID string, reason string)
}

func NewEngine(acct broker.Account, j journal.Journal) *Engine {
	return &Engine{
		acct:    acct,
		prices:  NewPriceStore(),
		trades:  make(map[string]*Trade),
		journal: j,
	}
}

// SetTradeClosedListener sets an optional listener to be notified when trades are auto-closed.
// The listener will be called after the trade is closed and the lock is released to avoid deadlocks.
func (e *Engine) SetTradeClosedListener(listener tradeClosedListener) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listener = listener
}

func (e *Engine) GetAccount(ctx context.Context) (broker.Account, error) {
	return e.acct, nil
}

func (e *Engine) Prices() *PriceStore { return e.prices }

func (e *Engine) GetPrice(ctx context.Context, instr string) (broker.Price, error) {
	return e.prices.Get(instr)
}

func (e *Engine) CreateMarketOrder(ctx context.Context, req broker.MarketOrderRequest) (broker.OrderFill, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, _ := e.prices.Get(req.Instrument)
	fillPrice := p.Ask
	if req.Units < 0 {
		fillPrice = p.Bid
	}

	id := id.New()

	trade := &Trade{
		ID:         id,
		Instrument: req.Instrument,
		Units:      req.Units,
		EntryPrice: fillPrice,
		StopLoss:   req.StopLoss,
		TakeProfit: req.TakeProfit,
		OpenTime:   p.Time,
		Open:       true,
	}
	e.trades[id] = trade

	return broker.OrderFill{
		TradeID:    id,
		Instrument: req.Instrument,
		Units:      req.Units,
		Price:      fillPrice,
	}, nil
}

// CloseTrade manually closes an open trade at the current market price.
// - Longs close on BID
// - Shorts close on ASK
// It records a TradeRecord and an EquitySnapshot, just like UpdatePrice().
func (e *Engine) CloseTrade(ctx context.Context, tradeID string, reason string) error {
	_ = ctx // reserved for future cancellation checks

	if reason == "" {
		reason = "ManualClose"
	}

	e.mu.Lock()
	
	t, ok := e.trades[tradeID]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("close trade: trade %q not found", tradeID)
	}
	if !t.Open {
		e.mu.Unlock()
		return fmt.Errorf("close trade: trade %q is already closed", tradeID)
	}

	p, err := e.prices.Get(t.Instrument)
	if err != nil {
		e.mu.Unlock()
		return fmt.Errorf("close trade: no price for %q: %w", t.Instrument, err)
	}

	// Correct side for closing
	closePrice := p.Bid
	if t.Units < 0 {
		closePrice = p.Ask
	}

	closeTime := p.Time
	if closeTime.IsZero() {
		closeTime = time.Now()
	}

	if err := e.closeTradeLocked(t, closePrice, closeTime, reason); err != nil {
		e.mu.Unlock()
		return err
	}

	// Revalue + margin, then snapshot (mirrors UpdatePrice())
	if err := e.revalueLocked(); err != nil {
		e.mu.Unlock()
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
		e.mu.Unlock()
		return err
	}

	if err := e.journal.RecordEquity(journal.EquitySnapshot{
		Time:        closeTime,
		Balance:     e.acct.Balance,
		Equity:      e.acct.Equity,
		MarginUsed:  e.acct.MarginUsed,
		FreeMargin:  e.acct.FreeMargin,
		MarginLevel: e.acct.MarginLevel,
	}); err != nil {
		e.mu.Unlock()
		return err
	}

	// Should be unnecessary after a close, but safe + consistent.
	// Note: liquidations during manual close are rare but possible
	liquidatedTradeIDs, err := e.enforceMarginLocked()
	
	// Capture listener before releasing lock to avoid race
	listener := e.listener
	
	e.mu.Unlock()
	
	// Notify listener about any liquidations that occurred
	if listener != nil {
		for _, tradeID := range liquidatedTradeIDs {
			listener.OnTradeClosed(tradeID, "LIQUIDATION")
		}
	}
	
	return err
}

// CloseAll manually closes all open trades at current market prices.
// - Longs close on BID
// - Shorts close on ASK
// It records a single EquitySnapshot after all closes (plus individual TradeRecords via closeTradeLocked).
func (e *Engine) CloseAll(ctx context.Context, reason string) error {
	_ = ctx // reserved for future cancellation checks

	if reason == "" {
		reason = "ManualClose"
	}

	e.mu.Lock()
	
	// Collect open trades first.
	open := make([]*Trade, 0, len(e.trades))
	for _, t := range e.trades {
		if t != nil && t.Open {
			open = append(open, t)
		}
	}
	if len(open) == 0 {
		e.mu.Unlock()
		return nil
	}

	// Preflight: ensure we have prices for all instruments we need to close.
	need := map[string]struct{}{}
	for _, t := range open {
		need[t.Instrument] = struct{}{}
	}
	for inst := range need {
		if _, err := e.prices.Get(inst); err != nil {
			e.mu.Unlock()
			return fmt.Errorf("close all: no price for %q: %w", inst, err)
		}
	}

	// Close each open trade using its instrument's latest price.
	var snapshotTime time.Time
	for _, t := range open {
		p, _ := e.prices.Get(t.Instrument)

		closePrice := p.Bid
		if t.Units < 0 {
			closePrice = p.Ask
		}

		closeTime := p.Time
		if closeTime.IsZero() {
			closeTime = time.Now()
		}
		if closeTime.After(snapshotTime) {
			snapshotTime = closeTime
		}

		if err := e.closeTradeLocked(t, closePrice, closeTime, reason); err != nil {
			e.mu.Unlock()
			return err
		}
	}

	// Revalue + margin, then snapshot (mirrors UpdatePrice / CloseTrade behavior).
	if err := e.revalueLocked(); err != nil {
		e.mu.Unlock()
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
		e.mu.Unlock()
		return err
	}

	if snapshotTime.IsZero() {
		snapshotTime = time.Now()
	}

	if err := e.journal.RecordEquity(journal.EquitySnapshot{
		Time:        snapshotTime,
		Balance:     e.acct.Balance,
		Equity:      e.acct.Equity,
		MarginUsed:  e.acct.MarginUsed,
		FreeMargin:  e.acct.FreeMargin,
		MarginLevel: e.acct.MarginLevel,
	}); err != nil {
		e.mu.Unlock()
		return err
	}

	// Should be unnecessary after closing everything, but consistent and safe.
	liquidatedTradeIDs, err := e.enforceMarginLocked()
	
	// Capture listener before releasing lock to avoid race
	listener := e.listener
	
	e.mu.Unlock()
	
	// Notify listener about any liquidations that occurred
	if listener != nil {
		for _, tradeID := range liquidatedTradeIDs {
			listener.OnTradeClosed(tradeID, "LIQUIDATION")
		}
	}
	
	return err
}

func (e *Engine) Revalue() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	equity := e.acct.Balance

	for _, t := range e.trades {
		if !t.Open {
			continue
		}

		p, err := e.prices.Get(t.Instrument)
		if err != nil {
			return err
		}

		// meta := market.Instruments[t.Instrument]

		// Use correct price side
		mark := p.Bid
		if t.Units < 0 {
			mark = p.Ask
		}

		rate, err := market.QuoteToAccountRate(
			t.Instrument,
			e.acct.Currency,
			e,
		)
		if err != nil {
			return err
		}

		equity += UnrealizedPL(*t, mark, rate)
	}

	e.acct.Equity = equity
	return nil
}

// sim/engine.go
func (e *Engine) UpdatePrice(p broker.Price) error {
	e.mu.Lock()
	
	e.prices.Set(p)

	// Collect trades to auto-close
	var closedTrades []struct {
		tradeID string
		reason  string
	}

	for _, t := range e.trades {
		if !t.Open || t.Instrument != p.Instrument {
			continue
		}

		// Correct price side
		mark := p.Bid
		if t.Units < 0 {
			mark = p.Ask
		}

		reason := ""
		switch {
		case hitStopLoss(t, mark):
			reason = "StopLoss"

		case hitTakeProfit(t, mark):
			reason = "TakeProfit"
		}
		if reason != "" {
			tradeID := t.ID
			if err := e.closeTradeLocked(t, mark, p.Time, reason); err != nil {
				e.mu.Unlock()
				return err
			}
			// Collect for notification after lock is released
			closedTrades = append(closedTrades, struct {
				tradeID string
				reason  string
			}{tradeID, reason})
		}
	}

	// Revalue + margin
	if err := e.revalueLocked(); err != nil {
		e.mu.Unlock()
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
		e.mu.Unlock()
		return err
	}

	err := e.journal.RecordEquity(journal.EquitySnapshot{
		Time:        p.Time,
		Balance:     e.acct.Balance,
		Equity:      e.acct.Equity,
		MarginUsed:  e.acct.MarginUsed,
		FreeMargin:  e.acct.FreeMargin,
		MarginLevel: e.acct.MarginLevel,
	})
	if err != nil {
		e.mu.Unlock()
		return err
	}

	// Forced liquidation if needed (returns liquidated trade IDs)
	liquidatedTradeIDs, err := e.enforceMarginLocked()
	
	// Capture listener before releasing lock to avoid race
	listener := e.listener
	
	e.mu.Unlock()
	
	// Notify listener about auto-closed trades after releasing lock to avoid deadlocks
	if listener != nil {
		for _, ct := range closedTrades {
			listener.OnTradeClosed(ct.tradeID, ct.reason)
		}
		// Also notify about liquidations
		for _, tradeID := range liquidatedTradeIDs {
			listener.OnTradeClosed(tradeID, "LIQUIDATION")
		}
	}
	
	return err
}

func (e *Engine) closeTradeLocked(t *Trade, closePrice float64, closeTime time.Time, reason string) error {

	rate, err := market.QuoteToAccountRate(
		t.Instrument,
		e.acct.Currency,
		e,
	)
	if err != nil {
		return err
	}

	pl := UnrealizedPL(*t, closePrice, rate)

	t.ClosePrice = closePrice
	t.CloseTime = closeTime
	t.RealizedPL = pl
	t.Open = false

	e.acct.Balance += pl

	e.journal.RecordTrade(journal.TradeRecord{
		TradeID:    t.ID,
		Instrument: t.Instrument,
		Units:      t.Units,
		EntryPrice: t.EntryPrice,
		ExitPrice:  closePrice,
		OpenTime:   t.OpenTime,
		CloseTime:  closeTime,
		RealizedPL: t.RealizedPL,
		Reason:     reason,
	})

	return nil
}

func (e *Engine) revalueLocked() error {
	equity := e.acct.Balance

	for _, t := range e.trades {
		if !t.Open {
			continue
		}

		p, err := e.prices.Get(t.Instrument)
		if err != nil {
			return err
		}

		mark := p.Bid
		if t.Units < 0 {
			mark = p.Ask
		}

		rate, err := market.QuoteToAccountRate(
			t.Instrument,
			e.acct.Currency,
			e,
		)
		if err != nil {
			return err
		}

		equity += UnrealizedPL(*t, mark, rate)
	}

	e.acct.Equity = equity
	return nil
}

func (e *Engine) recomputeMarginLocked() error {
	var used float64

	for _, t := range e.trades {
		if !t.Open {
			continue
		}

		p, err := e.prices.Get(t.Instrument)
		if err != nil {
			return err
		}

		rate, err := market.QuoteToAccountRate(
			t.Instrument,
			e.acct.Currency,
			e,
		)
		if err != nil {
			return err
		}

		used += TradeMargin(
			t.Units,
			p.Mid(), // margin uses mid
			t.Instrument,
			rate,
		)
	}

	e.acct.MarginUsed = used
	e.acct.FreeMargin = e.acct.Equity - used

	if used > 0 {
		e.acct.MarginLevel = e.acct.Equity / used
	} else {
		e.acct.MarginLevel = 0
	}

	return nil
}

func (e *Engine) enforceMarginLocked() ([]string, error) {
	var liquidatedTradeIDs []string
	
	for {
		if e.acct.MarginUsed <= 0 {
			return liquidatedTradeIDs, nil
		}

		if e.acct.Equity >= e.acct.MarginUsed {
			return liquidatedTradeIDs, nil
		}

		// Find worst open trade
		var worst *Trade
		var worstPL float64

		for _, t := range e.trades {
			if !t.Open {
				continue
			}

			p, _ := e.prices.Get(t.Instrument)
			mark := p.Bid
			if t.Units < 0 {
				mark = p.Ask
			}

			rate, _ := market.QuoteToAccountRate(
				t.Instrument,
				e.acct.Currency,
				e,
			)

			pl := UnrealizedPL(*t, mark, rate)

			if worst == nil || pl < worstPL {
				worst = t
				worstPL = pl
			}
		}

		if worst == nil {
			return liquidatedTradeIDs, nil
		}

		// Force close
		p, _ := e.prices.Get(worst.Instrument)
		closePrice := p.Bid
		if worst.Units < 0 {
			closePrice = p.Ask
		}

		tradeID := worst.ID
		if err := e.closeTradeLocked(worst, closePrice, p.Time, "LIQUIDATION"); err != nil {
			return liquidatedTradeIDs, err
		}
		liquidatedTradeIDs = append(liquidatedTradeIDs, tradeID)

		// Revalue after liquidation
		if err := e.revalueLocked(); err != nil {
			return liquidatedTradeIDs, err
		}
		if err := e.recomputeMarginLocked(); err != nil {
			return liquidatedTradeIDs, err
		}
	}
}
