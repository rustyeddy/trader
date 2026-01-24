package sim

import (
	"context"
	"sync"
	"time"

	"github.com/rustyeddy/trader/pkg/broker"
	"github.com/rustyeddy/trader/pkg/id"
	"github.com/rustyeddy/trader/pkg/journal"
	"github.com/rustyeddy/trader/pkg/market"
)

type Engine struct {
	mu      sync.Mutex
	acct    broker.Account
	prices  *PriceStore
	trades  map[string]*Trade
	nextID  int
	journal journal.Journal
}

func NewEngine(acct broker.Account, j journal.Journal) *Engine {
	return &Engine{
		acct:    acct,
		prices:  NewPriceStore(),
		trades:  make(map[string]*Trade),
		journal: j,
	}
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

// pkg/sim/engine.go
func (e *Engine) UpdatePrice(p broker.Price) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.prices.Set(p)

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
			return nil
		}

		// Force close
		p, _ := e.prices.Get(worst.Instrument)
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
