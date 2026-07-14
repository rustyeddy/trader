package datamanager

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// SeedCandles points the package-global store at a fresh temp directory
// (via UseTempDataDir) and writes one month of candles into it. It
// replaces the old NewStoreAt+WriteMonthlyCandles(+SwapStore) pattern for
// tests outside this package, which can no longer construct a store
// directly since store is unexported.
//
// To seed more than one month in the same test, call UseTempDataDir once
// followed by WriteCandles for each month, rather than calling SeedCandles
// more than once (each call points at a brand new, empty temp dir).
func SeedCandles(tb testing.TB, source, instrument string, tf types.Timeframe, monthStart time.Time, candles []market.Candle) {
	tb.Helper()

	UseTempDataDir(tb)
	WriteCandles(tb, source, instrument, tf, monthStart, candles)
}

// WriteCandles writes one month of candles into the currently active
// global store (established by UseTempDataDir or a prior SeedCandles call).
func WriteCandles(tb testing.TB, source, instrument string, tf types.Timeframe, monthStart time.Time, candles []market.Candle) {
	tb.Helper()

	if err := getStore().WriteMonthlyCandles(source, instrument, tf, monthStart, candles); err != nil {
		tb.Fatalf("WriteCandles: %v", err)
	}
}

// WriteCandleSet writes cs into the currently active global store. Exposed
// for tests that build a CandleSet directly (via NewMonthlyCandleSet)
// rather than going through SeedCandles/WriteCandles.
func WriteCandleSet(tb testing.TB, cs *CandleSet) {
	tb.Helper()

	if err := getStore().WriteCSV(cs); err != nil {
		tb.Fatalf("WriteCandleSet: %v", err)
	}
}

// UseTempDataDir points the package-global store at a fresh temporary
// directory for the duration of the test (via tb.Cleanup) and returns the
// directory path. Useful when a test needs to write fixture files directly
// (bypassing normal candle writes) rather than seed via SeedCandles.
func UseTempDataDir(tb testing.TB) string {
	tb.Helper()

	dir := tb.TempDir()
	restore := swapStore(newStoreAt(dir))
	tb.Cleanup(restore)
	return dir
}

// PathForMonthlyCandle returns the canonical path for a monthly candle key
// under the current global store. Exposed for tests that need to write or
// inspect fixture files directly.
func PathForMonthlyCandle(k Key) string {
	return getStore().PathForMonthlyCandle(k)
}
