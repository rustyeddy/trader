package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultTraderDataDir returns the per-user directory trader uses to
// compute a default --store-root/--raw-root when neither is
// explicitly configured (issue #141): $XDG_DATA_HOME/trader when
// XDG_DATA_HOME is set, otherwise ~/.local/share/trader — the XDG Base
// Directory convention, so trader's default data location matches
// where other modern Linux CLI tools already keep their own per-user
// data, rather than inventing a Trader-specific convention.
func defaultTraderDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "trader"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("trader: determining default data directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "trader"), nil
}

// applyDefaultDataRoots fills cfg.StoreRoot and cfg.RawRoot with a
// computed default (issue #141) whenever the caller left either empty
// -- meaning neither a flag nor TRADER_STORE_ROOT/TRADER_RAW_ROOT
// configured it, since neither field carries a config default:"value"
// tag. RawRoot's default is scoped under cfg.Provider (raw archives
// are provider-specific; a default RawRoot chosen before Provider's
// own default ("oanda") resolved would be wrong for a non-default
// provider).
//
// A directory it filled in itself is also created here
// (os.MkdirAll) if it does not already exist, so a fresh install's
// first read command finds a real, empty, readable directory rather
// than the raw filesystem error Coverage/Plan's own raw-archive
// inspection reports for a missing RawRoot (confirmed directly:
// Bars/Coverage tolerate a missing StoreRoot as "no data found," but
// Coverage/Plan do not tolerate a missing RawRoot the same way).
// A path the caller explicitly configured is never auto-created here:
// if it does not exist, that is preserved as the same error it always
// was, rather than silently creating whatever the caller actually
// mistyped.
func applyDefaultDataRoots(cfg *datasetConfig) error {
	needsStoreRoot := cfg.StoreRoot == ""
	needsRawRoot := cfg.RawRoot == ""
	if !needsStoreRoot && !needsRawRoot {
		return nil
	}

	dataDir, err := defaultTraderDataDir()
	if err != nil {
		return err
	}

	if needsStoreRoot {
		cfg.StoreRoot = filepath.Join(dataDir, "data")
		if err := os.MkdirAll(cfg.StoreRoot, 0o755); err != nil {
			return fmt.Errorf("trader: creating default store root %s: %w", cfg.StoreRoot, err)
		}
	}
	if needsRawRoot {
		cfg.RawRoot = filepath.Join(dataDir, "raw", cfg.Provider)
		if err := os.MkdirAll(cfg.RawRoot, 0o755); err != nil {
			return fmt.Errorf("trader: creating default raw root %s: %w", cfg.RawRoot, err)
		}
	}
	return nil
}
