package trader

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestDataDir is the testdata directory path relative to workspace root.
const TestDataDir = "testdata"

// GenerateSyntheticTestData creates a year of synthetic candle data in testdata.
// This is useful for reproducible trading tests without needing real market data.
// Call this from an init function or a test setup if the data doesn't exist.
func GenerateSyntheticTestData(basedir string, instrument string, year int, timeframe Timeframe) (*CandleSet, error) {
	if basedir == "" {
		basedir = TestDataDir
	}

	// Create synthetic data
	cfg := DefaultSyntheticConfig(instrument)
	cfg.Timeframe = timeframe
	cfg.Seed = int64(year) * 1000

	cs, err := cfg.GenerateSyntheticMonthlyCandles(year, time.January)
	if err != nil {
		return nil, fmt.Errorf("generate synthetic candles: %w", err)
	}

	// Write to CSV
	if err := os.MkdirAll(basedir, 0o755); err != nil {
		return nil, fmt.Errorf("create testdata dir: %w", err)
	}

	store := &Store{basedir: basedir}
	if err := store.WriteCSV(cs); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}

	return cs, nil
}

// GenerateSyntheticYearTestData generates a full year of synthetic test data.
func GenerateSyntheticYearTestData(basedir string, instrument string, year int, timeframe Timeframe) ([]string, error) {
	if basedir == "" {
		basedir = TestDataDir
	}

	cfg := DefaultSyntheticConfig(instrument)
	cfg.Timeframe = timeframe
	cfg.Seed = int64(year) * 1000

	if err := os.MkdirAll(basedir, 0o755); err != nil {
		return nil, fmt.Errorf("create testdata dir: %w", err)
	}

	store := &Store{basedir: basedir}
	return cfg.GenerateSyntheticYearlyAndWrite(store, year)
}

// TestHelperGenerateSyntheticCandles is a test helper to generate synthetic candles.
// Use this in tests to quickly set up reproducible candle data.
func TestHelperGenerateSyntheticCandles(t *testing.T, instrument string, year int, month time.Month, timeframe Timeframe) *CandleSet {
	t.Helper()
	cfg := DefaultSyntheticConfig(instrument)
	cfg.Timeframe = timeframe
	cfg.Seed = int64(year)*12 + int64(month)

	cs, err := cfg.GenerateSyntheticMonthlyCandles(year, month)
	if err != nil {
		t.Fatalf("generate synthetic candles: %v", err)
	}
	return cs
}

// TestHelperGenerateSyntheticCandlesWithConfig generates synthetic candles with custom config.
func TestHelperGenerateSyntheticCandlesWithConfig(t *testing.T, cfg SyntheticCandleConfig, year int, month time.Month) *CandleSet {
	t.Helper()
	cs, err := cfg.GenerateSyntheticMonthlyCandles(year, month)
	if err != nil {
		t.Fatalf("generate synthetic candles: %v", err)
	}
	return cs
}

// MakeSyntheticCandleSetIterator creates an iterator from a synthetic CandleSet.
// Useful for feeding TestTrader with synthetic data.
func MakeSyntheticCandleSetIterator(cs *CandleSet) CandleIterator {
	return NewCandleSetIterator(cs, TimeRange{})
}

// CreateTestDataFiles generates and writes synthetic CSV files to the testdata directory.
// Returns paths to created files.
// Example: CreateTestDataFiles("EURUSD", 2025, H1) creates all 12 months of hourly data.
func CreateTestDataFiles(instrument string, year int, timeframe Timeframe) ([]string, error) {
	testdataDir := TestDataDir
	return GenerateSyntheticYearTestData(testdataDir, instrument, year, timeframe)
}

// GetOrCreateTestData ensures test data exists, creating it if needed.
// Returns the path to the testdata directory.
func GetOrCreateTestData(instrument string, year int, timeframe Timeframe) (string, error) {
	testdataDir := TestDataDir
	store := &Store{basedir: testdataDir}

	// Check if data already exists
	key := Key{
		Instrument: NormalizeInstrument(instrument),
		Source:     "candles",
		Kind:       KindCandle,
		TF:         timeframe,
		Year:       year,
		Month:      1,
	}
	expectedPath := store.PathForAsset(key)
	if _, err := os.Stat(expectedPath); err == nil {
		return testdataDir, nil
	}

	// Create if missing
	if _, err := CreateTestDataFiles(instrument, year, timeframe); err != nil {
		return "", err
	}

	return testdataDir, nil
}

// LoadSyntheticCandles loads synthetic candles from testdata, creating them if needed.
func LoadSyntheticCandles(instrument string, year int, month time.Month, timeframe Timeframe) (*CandleSet, error) {
	testdataDir := TestDataDir
	store := &Store{basedir: testdataDir}

	key := Key{
		Instrument: NormalizeInstrument(instrument),
		Source:     "candles",
		Kind:       KindCandle,
		TF:         timeframe,
		Year:       year,
		Month:      int(month),
	}

	// Try to load existing
	cs, err := store.ReadCSV(key)
	if err == nil {
		return cs, nil
	}

	// Create and write if not found
	cfg := DefaultSyntheticConfig(instrument)
	cfg.Timeframe = timeframe
	cfg.Seed = int64(year)*12 + int64(month)

	cs, err = cfg.GenerateSyntheticMonthlyCandles(year, month)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(testdataDir, 0o755); err != nil {
		return nil, err
	}

	if err := store.WriteCSV(cs); err != nil {
		return nil, err
	}

	return cs, nil
}
