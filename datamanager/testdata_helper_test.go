package datamanager

import (
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// HelperGenerateSyntheticCandles is a test helper to generate synthetic candles.
func HelperGenerateSyntheticCandles(t *testing.T, instrument string, year int, month time.Month, timeframe types.Timeframe) *CandleSet {
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

// HelperGenerateSyntheticCandlesWithConfig generates synthetic candles with custom config.
func HelperGenerateSyntheticCandlesWithConfig(t *testing.T, cfg SyntheticCandleConfig, year int, month time.Month) *CandleSet {
	t.Helper()
	cs, err := cfg.GenerateSyntheticMonthlyCandles(year, month)
	if err != nil {
		t.Fatalf("generate synthetic candles: %v", err)
	}
	return cs
}

// MakeSyntheticCandleSetIterator creates an iterator from a synthetic CandleSet.
func MakeSyntheticCandleSetIterator(cs *CandleSet) market.CandleIterator {
	return newCandleSetIterator(cs, types.TimeRange{})
}

// CreateTestDataFiles generates and writes synthetic CSV files to the testdata directory.
func CreateTestDataFiles(instrument string, year int, timeframe types.Timeframe) ([]string, error) {
	testdataDir := TestDataDir
	return GenerateSyntheticYearTestData(testdataDir, instrument, year, timeframe)
}

// GetOrCreateTestData ensures test data exists, creating it if needed.
func GetOrCreateTestData(instrument string, year int, timeframe types.Timeframe) (string, error) {
	testdataDir := TestDataDir
	store := &store{basedir: testdataDir}

	key := Key{
		Instrument: market.NormalizeInstrument(instrument),
		Source:     "candles",
		Kind:       KindCandle,
		TF:         timeframe,
		Year:       year,
		Month:      1,
	}
	expectedPath, err := store.KeyPath(key)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(expectedPath); err == nil {
		return testdataDir, nil
	}

	if _, err := CreateTestDataFiles(instrument, year, timeframe); err != nil {
		return "", err
	}

	return testdataDir, nil
}

// LoadSyntheticCandles loads synthetic candles from testdata, creating them if needed.
func LoadSyntheticCandles(instrument string, year int, month time.Month, timeframe types.Timeframe) (*CandleSet, error) {
	testdataDir := TestDataDir
	store := &store{basedir: testdataDir}

	key := Key{
		Instrument: market.NormalizeInstrument(instrument),
		Source:     "candles",
		Kind:       KindCandle,
		TF:         timeframe,
		Year:       year,
		Month:      int(month),
	}

	cs, err := store.ReadCSV(key)
	if err == nil {
		return cs, nil
	}

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
