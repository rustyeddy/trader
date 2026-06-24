package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
)

type Sim struct {
	account *execution.Account
	journal journal.Journal
	prices  map[string]market.Tick
}

func NewSimBroker(acct *execution.Account, j journal.Journal) *Sim {
	if acct == nil {
		acct = execution.NewAccount("sim", 0)
	}
	if acct.Lots.All() == nil {
		acct.Lots = execution.LotBook{}
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

	marks := make(map[string]market.Price, len(e.prices))
	for instrument, px := range e.prices {
		marks[instrument] = px.Mid()
	}
	return e.account.ResolveWithMarks(marks)
}

func (e *Sim) CloseAll(ctx context.Context, reason string) error {
	if e == nil || e.account == nil {
		return fmt.Errorf("sim broker account is nil")
	}

	var lots []*execution.Lot
	_ = e.account.Lots.Range(func(lot *execution.Lot) error {
		lots = append(lots, lot)
		return nil
	})

	for _, lot := range lots {
		px, ok := e.prices[lot.Instrument]
		if !ok {
			return fmt.Errorf("no market price for %s", lot.Instrument)
		}
		exitPrice := px.Mid()
		exitTime := market.FromTime(time.Now().UTC())
		trade := &execution.Trade{
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
			Timestamp:   market.FromTime(time.Now().UTC()),
			Balance:     e.account.Balance,
			Equity:      e.account.Equity,
			MarginUsed:  e.account.MarginUsed,
			FreeMargin:  e.account.FreeMargin,
			MarginLevel: e.account.MarginLevel,
		})
	}

	return nil
}

func (e *Sim) GetAccount(context.Context) (*execution.Account, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	return e.account, nil
}
