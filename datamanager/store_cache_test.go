package datamanager

import (
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// TestStoreReadCSV_CachesPastMonth confirms a second ReadCSV for the same
// key is served from the in-memory cache rather than the filesystem: the
// underlying CSV is deleted after the first read, and the second read must
// still succeed and return equal contents.
func TestStoreReadCSV_CachesPastMonth(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cs := makeTestCandleSet(t, "EUR_USD", 2020, time.June, types.H1)
	cs.Candles[0].Candle = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}
	cs.SetValid(0)
	require.NoError(t, s.WriteCSV(cs))

	key := keyForSet(cs)
	first, err := s.ReadCSV(key)
	require.NoError(t, err)

	path, err := s.KeyPath(key)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	second, err := s.ReadCSV(key)
	require.NoError(t, err, "expected cached read to succeed after the CSV file was removed")
	require.Equal(t, first.Candles, second.Candles)
	require.Same(t, first, second, "expected the exact cached *CandleSet pointer to be returned")
}

// TestStoreReadCSV_SkipsCacheForCurrentMonth confirms the current calendar
// month is never served from cache, since it's a moving target that can
// change mid-process as new candles are downloaded.
func TestStoreReadCSV_SkipsCacheForCurrentMonth(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	cs := makeTestCandleSet(t, "EUR_USD", now.Year(), now.Month(), types.H1)
	cs.Candles[0].Candle = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}
	cs.SetValid(0)
	require.NoError(t, s.WriteCSV(cs))

	key := keyForSet(cs)
	_, err := s.ReadCSV(key)
	require.NoError(t, err)

	path, err := s.KeyPath(key)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	if n := time.Now().UTC(); n.Year() != now.Year() || n.Month() != now.Month() {
		t.Skip("UTC month rolled over mid-test; key is no longer the current month")
	}

	_, err = s.ReadCSV(key)
	require.Error(t, err, "current-month reads must never be served from cache")
}

// TestStoreWriteCSV_InvalidatesCache confirms a write for a key that was
// already cached is visible on the next read, rather than returning the
// stale cached CandleSet.
func TestStoreWriteCSV_InvalidatesCache(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cs := makeTestCandleSet(t, "EUR_USD", 2020, time.July, types.H1)
	cs.Candles[0].Candle = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}
	cs.SetValid(0)
	require.NoError(t, s.WriteCSV(cs))

	key := keyForSet(cs)
	first, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.Equal(t, types.Price(103), first.Candles[0].Close)

	cs.Candles[0].Candle = market.Candle{Open: 200, High: 205, Low: 199, Close: 203, Ticks: 1}
	require.NoError(t, s.WriteCSV(cs))

	second, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.Equal(t, types.Price(203), second.Candles[0].Close, "expected the rewritten value, not a stale cache hit")
}
