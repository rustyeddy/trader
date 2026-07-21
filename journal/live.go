package journal

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/types"
)

// liveJournalClient is the subset of brokers.Broker LiveJournal needs.
// Defined locally rather than importing brokers: brokers/sim already
// imports journal (Sim's journal field), so journal importing brokers
// back would cycle. Any brokers.Broker value (or *oanda.Client directly)
// satisfies this structurally, no explicit dependency required.
type liveJournalClient interface {
	StreamTransactions(ctx context.Context, accountID string, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error)
	GetTransactions(ctx context.Context, accountID string, sinceID int64) ([]oanda.Transaction, int64, error)
}

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
	client    liveJournalClient
	accountID string
	journal   Journal
	log       *slog.Logger
	// botIDLookup is called on each trade close to find which managed bot
	// opened the trade. Nil means no bot tagging.
	botIDLookup func(tradeID string) string

	mu           sync.Mutex
	pendingOpens map[string]*pendingOpen // by OANDA tradeID
	lastSeenTxID int64
}

type pendingOpen struct {
	Instrument string
	Units      types.Units
	EntryPrice types.Price
	OpenTime   types.Timestamp
}

// NewLiveJournal creates a journal worker. Call Run to start the subscription.
func NewLiveJournal(client liveJournalClient, accountID string, journal Journal, log *slog.Logger) *LiveJournal {
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

// SetBotIDLookup provides a function the journal calls on each close to find
// which bot opened a given OANDA trade ID. This lets the centralized journal
// (one per serve process) tag TradeRecords with the correct bot.
func (lj *LiveJournal) SetBotIDLookup(fn func(tradeID string) string) {
	lj.mu.Lock()
	lj.botIDLookup = fn
	lj.mu.Unlock()
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
		lj.noteLastSeenTxID(lastID)
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
			lj.noteLastSeenTxID(hb.LastTxID)
		},
	})
	if err != nil {
		return err
	}

	for ev := range ch {
		if err := lj.handleStreamEvent(ev); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (lj *LiveJournal) handleStreamEvent(ev oanda.TxEvent) error {
	if ev.Err != nil {
		lj.log.Warn("live-journal stream event error", "err", ev.Err)
		return ev.Err
	}
	lj.handleTransaction(ev.Tx)
	return nil
}

// handleTransaction routes a transaction to the open/close handler.
// Currently only ORDER_FILL is processed; other types advance the txID
// cursor but don't write to the journal.
func (lj *LiveJournal) handleTransaction(tx oanda.Transaction) {
	lj.noteLastSeenTxID(parseTxID(tx.ID))

	if tx.Type != "ORDER_FILL" {
		return
	}

	// Close fill(s): one ORDER_FILL can close multiple trades.
	for _, closed := range tx.TradesClosed {
		lj.recordClose(tx, closed)
	}

	// Some ORDER_FILL events can both close existing trades and open a new one.
	if tx.TradeID != "" {
		lj.recordOpen(tx)
	}
}

func (lj *LiveJournal) recordOpen(tx oanda.Transaction) {
	po := &pendingOpen{
		Instrument: tx.Instrument,
		Units:      types.Units(tx.Units),
		EntryPrice: types.PriceFromFloat(tx.Price),
		OpenTime:   types.FromTime(tx.Time),
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
	botIDLookup := lj.botIDLookup
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
			Instrument: tx.Instrument,
			Units:      types.Units(-closed.Units), // close units have opposite sign
		}
	}

	var botID string
	if botIDLookup != nil {
		botID = botIDLookup(closed.TradeID)
	}
	record := TradeRecord{
		TradeID:    closed.TradeID,
		BotID:      botID,
		Instrument: po.Instrument,
		Units:      po.Units,
		EntryPrice: po.EntryPrice,
		ExitPrice:  types.PriceFromFloat(closed.Price),
		OpenTime:   po.OpenTime,
		CloseTime:  types.FromTime(tx.Time),
		RealizedPL: types.MoneyFromFloat(closed.RealizedPL),
		Reason:     tx.Reason,
	}
	if err := lj.journal.RecordTrade(record); err != nil {
		lj.log.Error("live-journal RecordTrade failed",
			"trade_id", closed.TradeID,
			"err", err,
		)
		return
	}
	if ok {
		lj.mu.Lock()
		delete(lj.pendingOpens, closed.TradeID)
		lj.mu.Unlock()
	}
	lj.log.Info("live-journal trade recorded",
		"trade_id", closed.TradeID,
		"instrument", po.Instrument,
		"entry", po.EntryPrice.Float64(),
		"exit", closed.Price,
		"pl", closed.RealizedPL,
		"reason", tx.Reason,
	)
}

func (lj *LiveJournal) noteLastSeenTxID(id int64) {
	lj.mu.Lock()
	if id > lj.lastSeenTxID {
		lj.lastSeenTxID = id
	}
	lj.mu.Unlock()
}

func parseTxID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
