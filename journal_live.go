package trader

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// LiveJournal subscribes to an OANDA transaction stream and writes complete
// TradeRecord rows to the configured Journal as trades close.
//
// Open ORDER_FILL events are buffered in memory (keyed by tradeID) until
// the matching close ORDER_FILL arrives. The close fill provides the
// realized P/L; we look up the buffered open to fill in entry side and
// open time, then RecordTrade(...) writes the complete row.
//
// Heartbeats advance an in-memory "lastSeenTxID" cursor so callers can
// reconnect (or poll for gap recovery) from a known point.
type LiveJournal struct {
	client    *oanda.Client
	accountID string
	journal   Journal
	log       *slog.Logger

	mu          sync.Mutex
	pendingOpens map[string]*pendingOpen // by OANDA tradeID
	lastSeenTxID int64
}

type pendingOpen struct {
	TradeID    string
	Instrument string
	Units      int64 // signed; positive long, negative short
	EntryPrice float64
	OpenTime   time.Time
	Reason     string
}

// NewLiveJournal creates a journal worker. Call Run to start the subscription.
func NewLiveJournal(client *oanda.Client, accountID string, journal Journal, log *slog.Logger) *LiveJournal {
	if log == nil {
		log = slog.Default()
	}
	return &LiveJournal{
		client:       client,
		accountID:    accountID,
		journal:      journal,
		log:          log,
		pendingOpens: make(map[string]*pendingOpen),
	}
}

// LastSeenTxID returns the highest transaction ID we've processed (via
// heartbeat or actual transaction). Persist this for resume on restart.
func (lj *LiveJournal) LastSeenTxID() int64 {
	lj.mu.Lock()
	defer lj.mu.Unlock()
	return lj.lastSeenTxID
}

// Backfill polls GetTransactions from sinceID forward and replays them
// into the same handler used for streamed events. Call before Run to
// recover anything missed during downtime.
func (lj *LiveJournal) Backfill(ctx context.Context, sinceID int64) error {
	for {
		txns, lastID, err := lj.client.GetTransactions(ctx, lj.accountID, sinceID)
		if err != nil {
			return fmt.Errorf("live-journal backfill: %w", err)
		}
		for _, tx := range txns {
			lj.handleTransaction(tx)
		}
		lj.mu.Lock()
		if lastID > lj.lastSeenTxID {
			lj.lastSeenTxID = lastID
		}
		lj.mu.Unlock()
		// OANDA caps responses at 1000; if we got fewer we're done.
		if len(txns) < 1000 {
			return nil
		}
		sinceID = lastID
	}
}

// Run subscribes to the transaction stream and processes events until ctx
// is cancelled or the stream ends. Returns the final error from the stream
// (nil on clean ctx-cancel exit).
func (lj *LiveJournal) Run(ctx context.Context) error {
	ch, err := lj.client.StreamTransactions(ctx, lj.accountID, oanda.StreamOptions{
		OnHeartbeat: func(hb oanda.Heartbeat) {
			lj.mu.Lock()
			if hb.LastTxID > lj.lastSeenTxID {
				lj.lastSeenTxID = hb.LastTxID
			}
			lj.mu.Unlock()
		},
	})
	if err != nil {
		return err
	}

	for ev := range ch {
		if ev.Err != nil {
			lj.log.Warn("live-journal stream event error", "err", ev.Err)
			continue
		}
		lj.handleTransaction(ev.Tx)
	}
	return ctx.Err()
}

// handleTransaction routes a transaction to the open/close handler.
// Currently only ORDER_FILL is processed; other types advance the txID
// cursor but don't write to the journal.
func (lj *LiveJournal) handleTransaction(tx oanda.Transaction) {
	lj.mu.Lock()
	if id := parseTxID(tx.ID); id > lj.lastSeenTxID {
		lj.lastSeenTxID = id
	}
	lj.mu.Unlock()

	if tx.Type != "ORDER_FILL" {
		return
	}

	// Open fill: tradeOpened field present, tradesClosed empty.
	if tx.TradeID != "" && len(tx.TradesClosed) == 0 {
		lj.recordOpen(tx)
		return
	}

	// Close fill(s): one ORDER_FILL can close multiple trades.
	for _, closed := range tx.TradesClosed {
		lj.recordClose(tx, closed)
	}
}

func (lj *LiveJournal) recordOpen(tx oanda.Transaction) {
	po := &pendingOpen{
		TradeID:    tx.TradeID,
		Instrument: tx.Instrument,
		Units:      tx.Units,
		EntryPrice: tx.Price,
		OpenTime:   tx.Time,
		Reason:     tx.Reason,
	}
	lj.mu.Lock()
	lj.pendingOpens[tx.TradeID] = po
	lj.mu.Unlock()
	lj.log.Info("live-journal open recorded",
		"trade_id", tx.TradeID,
		"instrument", tx.Instrument,
		"units", tx.Units,
		"price", tx.Price,
	)
}

func (lj *LiveJournal) recordClose(tx oanda.Transaction, closed oanda.ClosedTrade) {
	lj.mu.Lock()
	po, ok := lj.pendingOpens[closed.TradeID]
	if ok {
		delete(lj.pendingOpens, closed.TradeID)
	}
	lj.mu.Unlock()

	if !ok {
		// We saw a close for a trade we didn't see open — could happen
		// if the journal started after the open. Record what we know.
		lj.log.Warn("live-journal close without prior open",
			"trade_id", closed.TradeID,
			"close_price", closed.Price,
			"realized_pl", closed.RealizedPL,
		)
		po = &pendingOpen{
			TradeID:    closed.TradeID,
			Instrument: tx.Instrument,
			Units:      -closed.Units, // close units have opposite sign
		}
	}

	record := TradeRecord{
		TradeID:    closed.TradeID,
		Instrument: po.Instrument,
		Units:      Units(po.Units),
		EntryPrice: PriceFromFloat(po.EntryPrice),
		ExitPrice:  PriceFromFloat(closed.Price),
		OpenTime:   FromTime(po.OpenTime),
		CloseTime:  FromTime(tx.Time),
		RealizedPL: MoneyFromFloat(closed.RealizedPL),
		Reason:     tx.Reason,
	}
	if err := lj.journal.RecordTrade(record); err != nil {
		lj.log.Error("live-journal RecordTrade failed",
			"trade_id", closed.TradeID,
			"err", err,
		)
		return
	}
	lj.log.Info("live-journal trade recorded",
		"trade_id", closed.TradeID,
		"instrument", po.Instrument,
		"entry", po.EntryPrice,
		"exit", closed.Price,
		"pl", closed.RealizedPL,
		"reason", tx.Reason,
	)
}

func parseTxID(s string) int64 {
	var id int64
	_, _ = fmt.Sscanf(s, "%d", &id)
	return id
}
