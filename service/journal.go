package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

// JournalConfig selects the journal backend and its destinations.
type JournalConfig struct {
	// Kind: "csv" or "sqlite"
	Kind string

	// CSV-only: paths to trades + equity CSV files.
	CSVTrades string
	CSVEquity string

	// SQLite-only: path to the database file.
	SQLitePath string
}

// OpenJournal opens the configured Journal. Caller is responsible for
// calling .Close() on the returned journal.
func (s *Service) OpenJournal(cfg JournalConfig) (trader.Journal, error) {
	switch cfg.Kind {
	case "csv":
		j, err := trader.NewCSV(cfg.CSVTrades, cfg.CSVEquity)
		if err != nil {
			return nil, fmt.Errorf("open csv journal: %w", err)
		}
		return j, nil
	case "sqlite":
		j, err := trader.NewSQLite(cfg.SQLitePath)
		if err != nil {
			return nil, fmt.Errorf("open sqlite journal: %w", err)
		}
		return j, nil
	default:
		return nil, fmt.Errorf("journal kind must be 'csv' or 'sqlite', got %q", cfg.Kind)
	}
}

// RunLiveJournal subscribes to the OANDA transaction stream and writes a
// TradeRecord per closed trade to the given Journal. Blocks until ctx is
// cancelled or the stream ends.
//
// If backfillFrom > 0, transactions with ID > backfillFrom are polled and
// replayed into the journal before the stream subscription starts —
// useful for downtime recovery.
func (s *Service) RunLiveJournal(ctx context.Context, journal trader.Journal, backfillFrom int64) (lastSeenTxID int64, err error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return 0, err
	}
	lj := trader.NewLiveJournal(s.OANDA, s.AccountID, journal, s.Log)

	if backfillFrom > 0 {
		if err := lj.Backfill(ctx, backfillFrom); err != nil {
			return lj.LastSeenTxID(), err
		}
	}

	runErr := lj.Run(ctx)
	if runErr != nil && ctx.Err() == nil {
		return lj.LastSeenTxID(), runErr
	}
	return lj.LastSeenTxID(), nil
}
