package service

import (
	"fmt"

	"github.com/rustyeddy/trader/journal"
)

// JournalConfig selects the journal backend and its destinations.
type JournalConfig struct {
	// Kind: "csv" or "json"
	Kind string

	// File-backed journals use one file for trades and one for equity snapshots.
	TradesPath string
	EquityPath string
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
	default:
		return nil, fmt.Errorf("journal kind must be 'csv' or 'json'; got %q", cfg.Kind)
	}
}
