package datamanager

import (
	"fmt"
	"os"

	"github.com/rustyeddy/trader/market"
)

// TestDataDir is the testdata directory path relative to workspace root.
const TestDataDir = "testdata"

// GenerateSyntheticYearTestData generates a full year of synthetic test data.
func GenerateSyntheticYearTestData(basedir string, instrument string, year int, timeframe market.Timeframe) ([]string, error) {
	if basedir == "" {
		basedir = TestDataDir
	}

	cfg := DefaultSyntheticConfig(instrument)
	cfg.Timeframe = timeframe
	cfg.Seed = int64(year) * 1000

	if err := os.MkdirAll(basedir, 0o755); err != nil {
		return nil, fmt.Errorf("create testdata dir: %w", err)
	}

	s := &store{basedir: basedir}
	return cfg.GenerateSyntheticYearlyAndWrite(s, year)
}
