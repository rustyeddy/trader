package journal

import "fmt"

// Config selects a journal backend and its destinations.
type Config struct {
	// Kind: "csv" or "json"
	Kind string

	// File-backed journals use one file for trades and one for equity snapshots.
	TradesPath string
	EquityPath string
}

// Open opens the Journal configured by cfg. Caller is responsible for
// calling .Close() on the returned journal.
func Open(cfg Config) (Journal, error) {
	switch cfg.Kind {
	case "csv":
		j, err := NewCSV(cfg.TradesPath, cfg.EquityPath)
		if err != nil {
			return nil, fmt.Errorf("open csv journal: %w", err)
		}
		return j, nil
	case "json":
		j, err := NewJSON(cfg.TradesPath, cfg.EquityPath)
		if err != nil {
			return nil, fmt.Errorf("open json journal: %w", err)
		}
		return j, nil
	default:
		return nil, fmt.Errorf("journal kind must be 'csv' or 'json'; got %q", cfg.Kind)
	}
}
