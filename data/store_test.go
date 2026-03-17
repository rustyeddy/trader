package data

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Basedir: t.TempDir()}
}

func newMonthlyCandleSet(t *testing.T, instrument string, year int, month time.Month, tf types.Timeframe) *market.CandleSet {
	t.Helper()

	instName := normalizeInstrument(instrument)
	inst := market.GetInstrument(instName)
	if inst == nil {
		inst = &market.Instrument{Name: instName}
	}

	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	cs, err := market.NewMonthlyCandleSet(
		inst,
		types.Timestamp(tf),
		types.FromTime(start),
		100_000,
		"test",
	)
	require.NoError(t, err)
	return cs
}

func keyForSet(cs *market.CandleSet) Key {
	start := time.Unix(int64(cs.Start), 0).UTC()
	return Key{
		Instrument: cs.Instrument.Name,
		Kind:       KindCandle,
		TF:         types.Timeframe(cs.Timeframe),
		Year:       start.Year(),
		Month:      int(start.Month()),
	}
}

func TestStoreWriteCSVReadCSVRoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cs := newMonthlyCandleSet(t, "EUR_USD", 2026, time.January, types.H1)

	cs.Candles[0] = market.Candle{
		High:      types.Price(101),
		Open:      types.Price(100),
		Low:       types.Price(99),
		Close:     types.Price(100),
		AvgSpread: types.Price(2),
		MaxSpread: types.Price(5),
		Ticks:     42,
	}
	cs.SetValid(0)

	cs.Candles[123] = market.Candle{
		High:      types.Price(205),
		Open:      types.Price(201),
		Low:       types.Price(200),
		Close:     types.Price(204),
		AvgSpread: types.Price(3),
		MaxSpread: types.Price(7),
		Ticks:     11,
	}
	cs.SetValid(123)

	require.NoError(t, s.WriteCSV(cs))

	key := keyForSet(cs)
	got, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, cs.Start, got.Start)
	require.Equal(t, cs.Timeframe, got.Timeframe)
	require.Equal(t, normalizeInstrument(cs.Instrument.Name), normalizeInstrument(got.Instrument.Name))

	require.Equal(t, cs.Candles[0], got.Candles[0])
	require.True(t, got.IsValid(0))

	require.Equal(t, cs.Candles[123], got.Candles[123])
	require.True(t, got.IsValid(123))

	require.False(t, got.IsValid(1))
}

func TestStoreReadCSVSkipsCommentsHeaderAndParsesFlags(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	key := Key{
		Instrument: "EUR_USD",
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       2026,
		Month:      1,
	}

	path := s.PathForAsset(key)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	raw := fmt.Sprintf(
		"# metadata line\nTimestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n%d,110,100,90,105,2,4,9,0x0001\n",
		ts,
	)
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o644))

	got, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, market.Candle{
		High:      types.Price(110),
		Open:      types.Price(100),
		Low:       types.Price(90),
		Close:     types.Price(105),
		AvgSpread: types.Price(2),
		MaxSpread: types.Price(4),
		Ticks:     9,
	}, got.Candles[0])
	require.True(t, got.IsValid(0))
}

func TestStoreReadCSVValidationAndRowErrors(t *testing.T) {
	t.Parallel()

	t.Run("rejects non-candle key", func(t *testing.T) {
		t.Parallel()
		s := newTestStore(t)

		_, err := s.ReadCSV(Key{
			Instrument: "EURUSD",
			Kind:       KindTick,
			TF:         types.Ticks,
			Year:       2026,
			Month:      1,
			Day:        1,
			Hour:       0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "only supports candle keys")
	})

	t.Run("rejects invalid month", func(t *testing.T) {
		t.Parallel()
		s := newTestStore(t)

		_, err := s.ReadCSV(Key{
			Instrument: "EURUSD",
			Kind:       KindCandle,
			TF:         types.M1,
			Year:       2026,
			Month:      13,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid candle key date")
	})

	t.Run("rejects short row", func(t *testing.T) {
		t.Parallel()
		s := newTestStore(t)
		key := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}

		path := s.PathForAsset(key)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(
			"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
				"1767225600,1,2\n",
		), 0o644))

		_, err := s.ReadCSV(key)
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected 9 fields")
	})

	t.Run("rejects misaligned timestamp", func(t *testing.T) {
		t.Parallel()
		s := newTestStore(t)
		key := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}

		path := s.PathForAsset(key)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(
			"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
				"1767225601,10,9,8,9,1,2,3,0x0001\n",
		), 0o644))

		_, err := s.ReadCSV(key)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not aligned")
	})
}

func TestStoreWriteCSVValidation(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	t.Run("nil candle set", func(t *testing.T) {
		t.Parallel()
		err := s.WriteCSV(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil candle set")
	})

	t.Run("nil instrument", func(t *testing.T) {
		t.Parallel()
		err := s.WriteCSV(&market.CandleSet{
			Timeframe: types.Timestamp(types.M1),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil candle set instrument")
	})

	t.Run("invalid timeframe", func(t *testing.T) {
		t.Parallel()
		err := s.WriteCSV(&market.CandleSet{
			Instrument: &market.Instrument{Name: "EURUSD"},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid candle set timeframe")
	})
}
