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
// This computes a path only -- it never creates a directory. An
// earlier version of this function called os.MkdirAll on a defaulted
// root, which review on #142 correctly identified as a real
// regression: read-only data commands (bars, coverage, plan) would
// then create directories on a fresh install, violating the
// no-hidden-writes invariant M2.5 established and regression-tested
// for read commands. The actual gap that MkdirAll was working around
// -- Coverage/Plan's raw-archive inspection failing outright on a
// RawRoot that does not exist yet -- is fixed at its real source
// instead: Manager's own rawInventoryLookup (marketdata/coverage.go)
// now treats a nonexistent rawRoot as an empty archive. Mutating
// commands need no help from this function either: Sync and Build
// each already create whatever directory they need themselves
// (oanda.WritePartition's and canonicalCSVStore.publish's own
// os.MkdirAll), exactly at the point they actually write, which is
// where directory creation as a side effect belongs.
func applyDefaultDataRoots(cfg *datasetConfig) error {
	if cfg.StoreRoot != "" && cfg.RawRoot != "" {
		return nil
	}

	dataDir, err := defaultTraderDataDir()
	if err != nil {
		return err
	}

	if cfg.StoreRoot == "" {
		cfg.StoreRoot = filepath.Join(dataDir, "data")
	}
	if cfg.RawRoot == "" {
		cfg.RawRoot = filepath.Join(dataDir, "raw", cfg.Provider)
	}
	return nil
}
