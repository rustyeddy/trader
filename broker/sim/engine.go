package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
)

type Engine struct {
	mu      sync.Mutex
	acct    broker.Account
	ticks   *market.TickStore
	trades  map[string]*Trade
	nextID  int
	journal journal.Journal
}

var (
	ErrTradeNotFound      = errors.New("trade not found")
	ErrTradeAlreadyClosed = errors.New("trade already closed")
)

func NewEngine(acct broker.Account, j journal.Journal) *Engine {
	return &Engine{
		acct:    acct,
		ticks:   market.NewTickStore(),
		trades:  make(map[string]*Trade),
		journal: j,
	}
}

func (e *Engine) GetAccount(ctx context.Context) (broker.Account, error) {
	return e.acct, nil
}

func (e *Engine) Prices() *market.TickStore {
	return e.ticks
}

// IsTradeOpen reports whether the given trade exists and is currently open.
func (e *Engine) IsTradeOpen(tradeID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.trades[tradeID]
	return ok && t != nil && t.Open
}

func (e *Engine) GetTick(ctx context.Context, instr string) (market.Tick, error) {
	return e.ticks.Get(instr)
}

func (e *Engine) CreateMarketOrder(ctx context.Context, req broker.MarketOrderRequest) (broker.OrderFill, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, _ := e.ticks.Get(req.Instrument)
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
	defer e.mu.Unlock()

	t, ok := e.trades[tradeID]
	if !ok {
		return fmt.Errorf("close trade: %w: %q", ErrTradeNotFound, tradeID)
	}
	if !t.Open {
		return fmt.Errorf("close trade: %w: %q", ErrTradeAlreadyClosed, tradeID)
	}

	p, err := e.ticks.Get(t.Instrument)
	if err != nil {
		return fmt.Errorf("close trade: no price for %q: %w", t.Instrument, err)
	}

	// Correct side for closing
	closePrice := p.Bid
	if t.Units < 0 {
		closePrice = p.Ask
	}

	closeTime := p.Time
	if closeTime == 0 {
		closeTime = market.Timestamp(time.Now().Unix())
	}

	if err := e.closeTradeLocked(t, closePrice, closeTime, reason); err != nil {
		return err
	}

	// Revalue + margin, then snapshot (mirrors UpdatePrice())
	if err := e.revalueLocked(); err != nil {
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
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
		return err
	}

	// Should be unnecessary after a close, but safe + consistent.
	return e.enforceMarginLocked()
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
	defer e.mu.Unlock()

	// Collect open trades first.
	open := make([]*Trade, 0, len(e.trades))
	for _, t := range e.trades {
		if t != nil && t.Open {
			open = append(open, t)
		}
	}
	if len(open) == 0 {
		return nil
	}

	// Preflight: ensure we have prices for all instruments we need to close.
	need := map[string]struct{}{}
	for _, t := range open {
		need[t.Instrument] = struct{}{}
	}
	for inst := range need {
		if _, err := e.ticks.Get(inst); err != nil {
			return fmt.Errorf("close all: no price for %q: %w", inst, err)
		}
	}

	// Close each open trade using its instrument's latest price.
	var snapshotTime market.Timestamp
	for _, t := range open {
		p, _ := e.ticks.Get(t.Instrument)

		closePrice := p.Bid
		if t.Units < 0 {
			closePrice = p.Ask
		}

		closeTime := p.Time
		if closeTime == 0 {
			closeTime = market.Timestamp(time.Now().Unix())
		}
		if closeTime > snapshotTime {
			snapshotTime = closeTime
		}

		if err := e.closeTradeLocked(t, closePrice, closeTime, reason); err != nil {
			return err
		}
	}

	// Revalue + margin, then snapshot (mirrors UpdatePrice / CloseTrade behavior).
	if err := e.revalueLocked(); err != nil {
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
		return err
	}

	if snapshotTime == 0 {
		snapshotTime = market.Timestamp(time.Now().Unix())
	}

	if err := e.journal.RecordEquity(journal.EquitySnapshot{
		Time:        snapshotTime,
		Balance:     e.acct.Balance,
		Equity:      e.acct.Equity,
		MarginUsed:  e.acct.MarginUsed,
		FreeMargin:  e.acct.FreeMargin,
		MarginLevel: e.acct.MarginLevel,
	}); err != nil {
		return err
	}

	// Should be unnecessary after closing everything, but consistent and safe.
	return e.enforceMarginLocked()
}

func (e *Engine) Revalue() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	equity := e.acct.Balance

	for _, t := range e.trades {
		if !t.Open {
			continue
		}

		p, err := e.ticks.Get(t.Instrument)
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
func (e *Engine) UpdatePrice(p market.Tick) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.ticks.Set(p)

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
			if err := e.closeTradeLocked(t, mark, p.Time, reason); err != nil {
				return err
			}
		}
	}

	// Revalue + margin
	if err := e.revalueLocked(); err != nil {
		return err
	}
	if err := e.recomputeMarginLocked(); err != nil {
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
		return err
	}

	// Forced liquidation if needed
	return e.enforceMarginLocked()
}

func (e *Engine) closeTradeLocked(t *Trade, closePrice market.Price, closeTime market.Timestamp, reason string) error {

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

		p, err := e.ticks.Get(t.Instrument)
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

		p, err := e.ticks.Get(t.Instrument)
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

func (e *Engine) enforceMarginLocked() error {
	for {
		if e.acct.MarginUsed <= 0 {
			return nil
		}

		if e.acct.Equity >= e.acct.MarginUsed {
			return nil
		}

		// Find worst open trade
		var worst *Trade
		var worstPL float64

		for _, t := range e.trades {
			if !t.Open {
				continue
			}

			p, _ := e.ticks.Get(t.Instrument)
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
			return nil
		}

		// Force close
		p, _ := e.ticks.Get(worst.Instrument)
		closePrice := p.Bid
		if worst.Units < 0 {
			closePrice = p.Ask
		}

		if err := e.closeTradeLocked(worst, closePrice, p.Time, "LIQUIDATION"); err != nil {
			return err
		}

		// Revalue after liquidation
		if err := e.revalueLocked(); err != nil {
			return err
		}
		if err := e.recomputeMarginLocked(); err != nil {
			return err
		}
	}
}
