package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader"
)

type Engine struct {
	account *trader.Account
	journal trader.Journal
	prices  map[string]trader.Tick
}

func NewEngine(acct trader.Account, j trader.Journal) *Engine {
	if acct.Positions.Positions() == nil {
		acct.Positions = trader.Positions{}
	}
	return &Engine{
		account: &acct,
		journal: j,
		prices:  make(map[string]trader.Tick),
	}
}

func (e *Engine) UpdatePrice(tick trader.Tick) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("nil engine")
	}
	inst := trader.NormalizeInstrument(tick.Instrument)
	if inst == "" {
		return fmt.Errorf("blank instrument")
	}
	tick.Instrument = inst
	e.prices[inst] = tick

	marks := make(map[string]trader.Price, len(e.prices))
	for instrument, px := range e.prices {
		marks[instrument] = px.Mid()
	}
	return e.account.ResolveWithMarks(marks)
}

func (e *Engine) CreateMarketOrder(ctx context.Context, req trader.OrderRequest) (*trader.Position, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("nil engine")
	}
	inst := trader.NormalizeInstrument(req.Instrument)
	if inst == "" {
		return nil, fmt.Errorf("blank instrument")
	}
	px, ok := e.prices[inst]
	if !ok {
		return nil, fmt.Errorf("no market price for %s", inst)
	}

	th := trader.NewTradeHistory(inst)
	units := req.Units
	if units == 0 {
		return nil, fmt.Errorf("units must be non-zero")
	}
	if units < 0 {
		th.Side = trader.Short
		units = -units
	} else {
		th.Side = trader.Long
	}
	th.Units = units

	pos := &trader.Position{
		TradeCommon: th.TradeCommon,
		FillPrice:   px.Mid(),
		FillTime:    trader.FromTime(time.Now().UTC()),
		State:       trader.PositionOpen,
	}
	if err := e.account.AddPosition(ctx, pos); err != nil {
		return nil, err
	}
	return pos, nil
}

func (e *Engine) CloseAll(ctx context.Context, reason string) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("nil engine")
	}

	var positions []*trader.Position
	_ = e.account.Positions.Range(func(pos *trader.Position) error {
		positions = append(positions, pos)
		return nil
	})

	for _, pos := range positions {
		px, ok := e.prices[pos.Instrument]
		if !ok {
			return fmt.Errorf("no market price for %s", pos.Instrument)
		}
		trade := &trader.Trade{
			TradeCommon: pos.TradeCommon,
			FillPrice:   px.Mid(),
			FillTime:    trader.FromTime(time.Now().UTC()),
		}
		if err := e.account.ClosePosition(pos, trade); err != nil {
			return err
		}
		if e.journal != nil {
			_ = e.journal.RecordTrade(trader.TradeRecord{
				TradeID:    trade.ID,
				Instrument: trade.Instrument,
				Units:      trade.Units,
				EntryPrice: pos.FillPrice,
				ExitPrice:  trade.FillPrice,
				OpenTime:   pos.FillTime,
				CloseTime:  trade.FillTime,
				RealizedPL: trade.PNL,
				Reason:     reason,
			})
		}
	}

	if e.journal != nil {
		_ = e.journal.RecordEquity(trader.EquitySnapshot{
			Timestamp:   trader.FromTime(time.Now().UTC()),
			Balance:     e.account.Balance,
			Equity:      e.account.Equity,
			MarginUsed:  e.account.MarginUsed,
			FreeMargin:  e.account.FreeMargin,
			MarginLevel: e.account.MarginLevel,
		})
	}

	return nil
}

func (e *Engine) GetAccount(context.Context) (*trader.Account, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("nil engine")
	}
	return e.account, nil
}
