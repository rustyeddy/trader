package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader"
)

type Sim struct {
	account *trader.Account
	journal trader.Journal
	prices  map[string]trader.Tick
}

func NewSimBroker(acct *trader.Account, j trader.Journal) *Sim {
	if acct == nil {
		acct = &trader.Account{}
	}
	if acct.Lots.All() == nil {
		acct.Lots = trader.LotBook{}
	}
	return &Sim{
		account: acct,
		journal: j,
		prices:  make(map[string]trader.Tick),
	}
}

func (e *Sim) UpdatePrice(tick trader.Tick) error {
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

func (e *Sim) CreateMarketOrder(ctx context.Context, req trader.OrderRequest) (*trader.Lot, error) {
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

	entryPrice := px.Mid()
	entryTime := trader.FromTime(time.Now().UTC())
	lot := &trader.Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     entryPrice,
		EntryTime:      entryTime,
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          trader.LotOpen,
	}
	if err := e.account.AddLot(ctx, lot); err != nil {
		return nil, err
	}
	return lot, nil
}

func (e *Sim) CloseAll(ctx context.Context, reason string) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("nil engine")
	}

	var lots []*trader.Lot
	_ = e.account.Lots.Range(func(lot *trader.Lot) error {
		lots = append(lots, lot)
		return nil
	})

	for _, lot := range lots {
		px, ok := e.prices[lot.Instrument]
		if !ok {
			return fmt.Errorf("no market price for %s", lot.Instrument)
		}
		exitPrice := px.Mid()
		exitTime := trader.FromTime(time.Now().UTC())
		trade := &trader.Trade{
			TradeCommon: lot.TradeCommon,
			EntryPrice:  lot.EntryPrice,
			EntryTime:   lot.EntryTime,
			ExitPrice:   exitPrice,
			ExitTime:    exitTime,
		}
		if err := e.account.CloseLot(lot, trade); err != nil {
			return err
		}
		if e.journal != nil {
			_ = e.journal.RecordTrade(trader.TradeRecord{
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

func (e *Sim) GetAccount(context.Context) (*trader.Account, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("nil engine")
	}
	return e.account, nil
}
