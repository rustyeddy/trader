// Package active persists the CLI's locally-selected default broker and
// account — set via `trader account default --broker --account-id` and
// read as the lowest-priority fallback (beneath explicit flags, env vars,
// and ~/.config/trader/*.yml) when a command needs a target and none was
// given explicitly.
//
// CLI-only. REST never consults this package — every REST request must
// carry an explicit broker and account, no defaulting.
package active

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Selection is the persisted default broker/account choice.
type Selection struct {
	Broker    string `json:"broker"`
	AccountID string `json:"account_id"`
}

// IsZero reports whether no selection has been made.
func (s Selection) IsZero() bool {
	return s.Broker == "" && s.AccountID == ""
}

// path returns the file Load/Save operate on: ~/.config/trader/active.json.
// Deliberately a different extension than the *.yml files
// config.LoadGlobalConfig globs and merges, so this never participates in
// that system.
func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("active: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "trader", "active.json"), nil
}

// Load reads the persisted selection. Returns a zero Selection, not an
// error, if the file doesn't exist yet.
func Load() (Selection, error) {
	p, err := path()
	if err != nil {
		return Selection{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Selection{}, nil
		}
		return Selection{}, fmt.Errorf("active: read %q: %w", p, err)
	}
	var sel Selection
	if err := json.Unmarshal(data, &sel); err != nil {
		return Selection{}, fmt.Errorf("active: parse %q: %w", p, err)
	}
	return sel, nil
}

// Save persists sel, creating ~/.config/trader/ if needed.
func Save(sel Selection) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("active: create config dir: %w", err)
	}
	data, err := json.MarshalIndent(sel, "", "  ")
	if err != nil {
		return fmt.Errorf("active: encode selection: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("active: write %q: %w", p, err)
	}
	return nil
}
