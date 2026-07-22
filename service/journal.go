package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/journal"
)

// JournalConfig selects the journal backend and its destinations.
type JournalConfig struct {
	// Kind: "csv", "json", or "postgres"
	Kind string

	// File-backed journals use one file for trades and one for equity snapshots.
	TradesPath string
	EquityPath string

	PostgresURL string
}

// OpenJournal opens the configured Journal. Caller is responsible for
// calling .Close() on the returned journal.
func (s *Service) OpenJournal(cfg JournalConfig) (journal.Journal, error) {
	switch cfg.Kind {
	case "csv":
		j, err := journal.NewCSV(cfg.TradesPath, cfg.EquityPath)
		if err != nil {
			return nil, fmt.Errorf("open csv journal: %w", err)
		}
		return j, nil
	case "json":
		j, err := journal.NewJSON(cfg.TradesPath, cfg.EquityPath)
		if err != nil {
			return nil, fmt.Errorf("open json journal: %w", err)
		}
		return j, nil
	case "postgres":
		return nil, fmt.Errorf("postgres journal not yet implemented")
	default:
		return nil, fmt.Errorf("journal kind must be 'csv', 'json', or 'postgres'; got %q", cfg.Kind)
	}
}

// RunLiveJournal subscribes the default account's transaction stream to the
// given journal. See account.Account.RunLiveJournal.
func (s *Service) RunLiveJournal(ctx context.Context, jrnl journal.Journal, backfillFrom int64) (lastSeenTxID int64, err error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return 0, err
	}
	return acc.RunLiveJournal(ctx, jrnl, backfillFrom, s.LookupTradeBotID)
}
