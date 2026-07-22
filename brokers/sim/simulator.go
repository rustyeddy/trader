package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// compile-time assertions: *Sim satisfies brokers.Broker and brokers.PriceUpdater.
// Asserted here rather than in package brokers, to avoid an import cycle
// (brokers/sim already depends on account, which depends on brokers for the
// Broker type itself).
var (
	_ brokers.Broker       = (*Sim)(nil)
	_ brokers.PriceUpdater = (*Sim)(nil)
)

// eventQueueSize mirrors account.Account's brokerEventQueueSize (same
// producer/consumer channel pattern, one level down — see
// StreamTransactions).
const eventQueueSize = 1024

// Sim is a simulated Broker (brokers.Broker): it fills orders against its
// own tracked prices (via UpdatePrice, or synthesized from candles via
// TickFromCandle) instead of a real network round-trip, using the same
// account.Account-adjacent bookkeeping (Account.AddLot/CloseLot) a real
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

	// events is StreamTransactions' feed: every fill (SubmitMarketOrder,
	// CloseTrade, or a stop/take triggered internally by UpdatePrice)
	// pushes here. A resting stop-loss only "happens" when price actually
	// reaches it — Sim is the thing that knows that, the same way a real
	// broker's book does, so it — not a caller — decides when that event
	// fires. Buffered and non-blocking on send (see emitFill): nothing in
	// this chunk drains it yet, so a full channel must drop, not block a
	// fill.
	events chan oanda.TxEvent
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
		events:  make(chan oanda.TxEvent, eventQueueSize),
	}
}

// emitFill pushes a fill transaction onto the event stream, dropping it
// (with a log line) rather than blocking if nothing has drained the
// channel — mirrors account.Account.emitEvent's full-queue behavior one
// level down.
func (e *Sim) emitFill(tx oanda.Transaction) {
	select {
	case e.events <- oanda.TxEvent{Tx: tx}:
	default:
		log.L.Warn("sim: dropping fill event, event queue is full", "type", tx.Type, "tradeID", tx.TradeID)
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
	if err := e.account.ResolveWithMarks(marks); err != nil {
		return err
	}

	return e.checkStopsAndTakes(inst, tick)
}

// checkStopsAndTakes closes any open lot on instrument whose Stop/Take was
// just crossed by tick — a resting stop/take only "happens" once price
// actually reaches it, so this runs on every price update rather than
// being decided synchronously when the stop was set. Mirrors
// backtest/exits.go's checkExit long/short asymmetry (stop-first on a
// same-tick double hit), adapted for bid/ask instead of a candle's OHLC:
// closing a long fills at bid, closing a short fills at ask — the same
// convention CloseTrade already uses.
func (e *Sim) checkStopsAndTakes(inst string, tick market.Tick) error {
	var closeErr error
	_ = e.account.Lots.Range(func(lot *account.Lot) error {
		if closeErr != nil || lot.Instrument != inst {
			return nil
		}
		hasStop := lot.Stop != 0
		hasTake := lot.Take != 0
		if !hasStop && !hasTake {
			return nil
		}

		var exitPrice types.Price
		var reason string
		switch lot.Side {
		case types.Long:
			stopHit := hasStop && tick.Bid <= lot.Stop
			takeHit := hasTake && tick.Bid >= lot.Take
			switch {
			case stopHit:
				exitPrice, reason = lot.Stop, "STOP"
			case takeHit:
				exitPrice, reason = lot.Take, "TAKE"
			default:
				return nil
			}
		case types.Short:
			stopHit := hasStop && tick.Ask >= lot.Stop
			takeHit := hasTake && tick.Ask <= lot.Take
			switch {
			case stopHit:
				exitPrice, reason = lot.Stop, "STOP"
			case takeHit:
				exitPrice, reason = lot.Take, "TAKE"
			default:
				return nil
			}
		default:
			return nil
		}

		exitPrice += account.FillAdjust(lot.Side == types.Short, 0, e.Slippage)
		if _, err := e.closeLotAndEmit(lot, exitPrice, tick.Timestamp, reason); err != nil {
			closeErr = err
		}
		return nil
	})
	return closeErr
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
// same bookkeeping path account.Account.SubmitOpen uses.
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

	e.emitFill(oanda.Transaction{
		Type:       "ORDER_FILL",
		AccountID:  accountID,
		Time:       px.Timestamp.Time(),
		Instrument: inst,
		Units:      units,
		Price:      fillPrice.Float64(),
		OrderID:    lot.ID,
		TradeID:    lot.ID,
	})

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
// account.Account's own partial-close handling (if any) needs mirroring.
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

	return e.closeLotAndEmit(lot, exitPrice, px.Timestamp, "sim close")
}

// closeLotAndEmit is the single close-and-notify path both CloseTrade and
// checkStopsAndTakes use: realize P/L via Account.CloseLot, record the
// journal entry, and emit the fill event — the same three things happen
// whether the close was caller-requested or a resting stop/take firing on
// its own.
func (e *Sim) closeLotAndEmit(lot *account.Lot, exitPrice types.Price, exitTime types.Timestamp, reason string) (*oanda.CloseTradeResult, error) {
	trade := &account.Trade{
		TradeCommon: lot.TradeCommon.Clone(),
		EntryPrice:  lot.EntryPrice,
		EntryTime:   lot.EntryTime,
		ExitPrice:   exitPrice,
		ExitTime:    exitTime,
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
			Reason:     reason,
		})
	}

	closedUnits := int64(trade.Units)
	if trade.Side == types.Long {
		closedUnits = -closedUnits
	}
	e.emitFill(oanda.Transaction{
		Type:       "ORDER_FILL",
		Time:       exitTime.Time(),
		Reason:     reason,
		Instrument: trade.Instrument,
		Units:      closedUnits,
		Price:      exitPrice.Float64(),
		PL:         trade.PNL.Float64(),
		TradeID:    trade.ID,
		TradesClosed: []oanda.ClosedTrade{{
			TradeID:    trade.ID,
			Units:      closedUnits,
			Price:      exitPrice.Float64(),
			RealizedPL: trade.PNL.Float64(),
		}},
	})

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

// StreamTransactions returns Sim's own fill feed (see the events field's
// doc comment) — every SubmitMarketOrder/CloseTrade fill and every
// stop/take triggered internally by UpdatePrice arrives here, the same
// role oanda.Client.StreamTransactions plays for a real account. Unlike
// the real client, this never closes on its own (no reconnect/error case
// to model) — it lives as long as Sim does.
func (e *Sim) StreamTransactions(ctx context.Context, accountID string, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	if e == nil || e.account == nil {
		return nil, fmt.Errorf("sim broker account is nil")
	}
	return e.events, nil
}
