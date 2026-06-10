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
		acct = trader.NewAccount("sim", 0)
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
		return fmt.Errorf("sim broker account is nil")
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

func (e *Sim) CloseAll(ctx context.Context, reason string) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("sim broker account is nil")
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
		return nil, fmt.Errorf("sim broker account is nil")
	}
	return e.account, nil
}
