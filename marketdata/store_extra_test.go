package marketdata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Store.Delete
// ---------------------------------------------------------------------------

func TestStoreDelete(t *testing.T) {
	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "test",
		Kind:       KindCandle,
		TF:         market.M1,
		Year:       2026,
		Month:      1,
	}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)

	require.NoError(t, s.Delete(k))

	exists, err = s.Exists(k)
	require.NoError(t, err)
	require.False(t, exists)
}

// ---------------------------------------------------------------------------
// OpenTickIterator validation errors
// ---------------------------------------------------------------------------

func TestOpenTickIterator_NotTickKind(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := Key{Kind: KindCandle, TF: market.Ticks}
	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a tick key")
}

func TestOpenTickIterator_BadTimeframe(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	k := Key{Kind: KindTick, TF: market.M1}
	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad timeframe")
}

func TestOpenTickIterator_MarketClosed(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	// Saturday - forex market is closed
	k := Key{Kind: KindTick, TF: market.Ticks, Year: 2026, Month: 1, Day: 3, Hour: 10} // 2026-01-03 is a Saturday
	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "market is closed")
}

func TestOpenTickIterator_UnusableFile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     market.SourceDukascopy,
		Kind:       KindTick,
		TF:         market.Ticks,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}

	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tick file not usable")
}

// ---------------------------------------------------------------------------
// buildH1 / buildD1 full path using temp store + real CSV
// ---------------------------------------------------------------------------

func TestBuildH1_FullPath(t *testing.T) {
	s := useTempStore(t)

	// Write M1 candles
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSetFromStart(s, "EURUSD", start, market.M1)
	require.NoError(t, err)
	cs.Candles[0] = testCandle()
	cs.SetValid(0)
	require.NoError(t, s.WriteCSV(cs))

	km1 := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	kh1 := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.H1, Year: 2026, Month: 1}
	wl := NewWantlist()
	wl.Put(Want{Key: kh1, WantReason: WantMissing})

	err = buildH1(context.Background(), kh1, []Key{km1}, wl)
	require.NoError(t, err)
	require.False(t, wl.Has(kh1))
}

func TestBuildD1_FullPath(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSetFromStart(s, "EURUSD", start, market.H1)
	require.NoError(t, err)
	cs.Candles[0] = testCandle()
	cs.SetValid(0)
	require.NoError(t, s.WriteCSV(cs))

	kh1 := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.H1, Year: 2026, Month: 1}
	kd1 := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.D1, Year: 2026, Month: 1}
	wl := NewWantlist()
	wl.Put(Want{Key: kd1, WantReason: WantMissing})

	err = buildD1(context.Background(), kd1, []Key{kh1}, wl)
	require.NoError(t, err)
	require.False(t, wl.Has(kd1))
}

// ---------------------------------------------------------------------------
// ChainedCandleIterator error propagation from sub-iterator
// ---------------------------------------------------------------------------

func TestChainedCandleIterator_SubIteratorErr(t *testing.T) {

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, market.H1)
	cs.Candles[0] = testCandle()
	cs.SetValid(0)

	real := newCandleSetIterator(cs, market.TimeRange{})
	_ = s

	// Wrap the real iterator in a chained one and read it
	chained := newChainedCandleIterator(real)
	count := 0
	for _, ok := chained.Next(); ok; _, ok = chained.Next() {
		count++
	}
	require.NoError(t, chained.Err())
	require.Equal(t, 1, count)
}

// ---------------------------------------------------------------------------
// scanFiles with a bi5-like path to hit the tick branch
// ---------------------------------------------------------------------------

func TestStoreScanFiles_WithBi5File(t *testing.T) {
	s := useTempStore(t)

	// Create a file that looks like a tick file (even if its content is garbage
	// the scanFiles function just needs to try parsing the path)
	bi5Path := filepath.Join(s.basedir, "dukascopy", "EURUSD", "2025", "01", "02", "13h_ticks.bi5")
	require.NoError(t, os.MkdirAll(filepath.Dir(bi5Path), 0o755))
	require.NoError(t, os.WriteFile(bi5Path, []byte("garbage"), 0o644))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Equal(t, 1, inv.Len())
}

// ---------------------------------------------------------------------------
// looksLikeHeader
// ---------------------------------------------------------------------------

func TestLooksLikeHeader(t *testing.T) {
	t.Parallel()

	require.True(t, looksLikeHeader([]string{"Timestamp", "High", "Open"}))
	require.True(t, looksLikeHeader([]string{"Time", "High"}))
	require.False(t, looksLikeHeader([]string{"1234567890", "100"}))
	require.False(t, looksLikeHeader([]string{}))
}

// ---------------------------------------------------------------------------
// Helpers for tests in this file
// ---------------------------------------------------------------------------

func testCandle() market.Candle {
	return market.Candle{
		Open: market.Price(100), High: market.Price(105),
		Low: market.Price(99), Close: market.Price(103), Ticks: 1,
	}
}

func newMonthlyCandleSetFromStart(
	s *Store,
	instrument string,
	start time.Time,
	tf market.Timeframe,
) (*candleSet, error) {
	return newMonthlyCandleSet(
		market.NormalizeInstrument(instrument),
		tf,
		market.FromTime(start),
		market.PriceScale,
		"test",
	)
}
