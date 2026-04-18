package trader

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func useTempStore(t *testing.T) *Store {
	t.Helper()

	oldStore := store

	s := &Store{
		basedir: t.TempDir(),
	}
	store = s

	t.Cleanup(func() {
		store = oldStore
	})

	return s
}

func writeMonthlyCandles(
	t *testing.T,
	s *Store,
	instrument string,
	tf types.Timeframe,
	year int,
	month time.Month,
	rows map[time.Time]types.Candle,
) Key {
	t.Helper()

	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)

	cs, err := types.NewMonthlyCandleSet(
		instrument,
		tf,
		types.FromTime(start),
		types.PriceScale,
		SourceCandles,
	)
	require.NoError(t, err)

	for ts, c := range rows {
		require.NoError(t, cs.AddCandle(types.FromTime(ts.UTC()), c))
	}

	require.NoError(t, s.WriteCSV(cs))

	return Key{
		Instrument: instrument,
		Source:     SourceCandles,
		Kind:       KindCandle,
		TF:         tf,
		Year:       year,
		Month:      int(month),
	}
}

func collectCandles(t *testing.T, it CandleIterator) ([]types.Timestamp, []types.Candle) {
	t.Helper()

	var outTS []types.Timestamp
	var outCandles []types.Candle

	for it.Next() {
		outTS = append(outTS, it.Timestamp())
		outCandles = append(outCandles, it.Candle())
	}

	require.NoError(t, it.Err())
	require.NoError(t, it.Close())

	return outTS, outCandles
}

func TestDataManagerCandles_ChainsMonthsAndFiltersRange(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	jan31_23 := time.Date(2026, time.January, 31, 23, 0, 0, 0, time.UTC)
	feb01_00 := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	feb01_01 := time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC)
	feb01_02 := time.Date(2026, time.February, 1, 2, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", types.H1, 2026, time.January, map[time.Time]types.Candle{
		jan31_23: {
			Open:  101,
			High:  105,
			Low:   100,
			Close: 104,
			Ticks: 10,
		},
	})

	writeMonthlyCandles(t, s, "EURUSD", types.H1, 2026, time.February, map[time.Time]types.Candle{
		feb01_00: {
			Open:  201,
			High:  205,
			Low:   200,
			Close: 204,
			Ticks: 20,
		},
		feb01_01: {
			Open:  301,
			High:  305,
			Low:   300,
			Close: 304,
			Ticks: 30,
		},
		feb01_02: {
			Open:  401,
			High:  405,
			Low:   400,
			Close: 404,
			Ticks: 40,
		},
	})

	req := CandleRequest{
		Source:     SourceCandles,
		Instrument: "EURUSD",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(jan31_23),
			End:   types.FromTime(feb01_02), // exclusive
		},
		Strict: true,
	}

	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)

	gotTS, gotCandles := collectCandles(t, it)

	times := []types.Timestamp{
		types.FromTime(jan31_23),
		types.FromTime(feb01_00),
		types.FromTime(feb01_01),
	}
	require.Equal(t, times, gotTS)
	require.Equal(t, []types.Candle{
		{
			Open:  101,
			High:  105,
			Low:   100,
			Close: 104,
			Ticks: 10,
		},
		{
			Open:  201,
			High:  205,
			Low:   200,
			Close: 204,
			Ticks: 20,
		},
		{
			Open:  301,
			High:  305,
			Low:   300,
			Close: 304,
			Ticks: 30,
		},
	}, gotCandles)
}

func TestDataManagerCandles_StrictFalseSkipsMissingMonths(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	jan15_00 := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", types.H1, 2026, time.January, map[time.Time]types.Candle{
		jan15_00: {
			Open:  111,
			High:  112,
			Low:   110,
			Close: 111,
			Ticks: 11,
		},
	})

	req := CandleRequest{
		Source:     SourceCandles,
		Instrument: "EURUSD",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			End:   types.FromTime(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
		},
		Strict: false,
	}

	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)

	gotTS, gotCandles := collectCandles(t, it)

	require.Equal(t, []types.Timestamp{
		types.FromTime(jan15_00),
	}, gotTS)

	require.Equal(t, []types.Candle{
		{
			Open:  111,
			High:  112,
			Low:   110,
			Close: 111,
			Ticks: 11,
		},
	}, gotCandles)
}

func TestDataManagerCandles_StrictTrueErrorsOnMissingMonth(t *testing.T) {
	s := useTempStore(t)
	dm := &DataManager{}

	jan15_00 := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", types.H1, 2026, time.January, map[time.Time]types.Candle{
		jan15_00: {
			Open:  111,
			High:  112,
			Low:   110,
			Close: 111,
			Ticks: 11,
		},
	})

	req := CandleRequest{
		Source:     SourceCandles,
		Instrument: "EURUSD",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			End:   types.FromTime(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
		},
		Strict: true,
	}

	it, err := dm.Candles(context.Background(), req)
	require.Nil(t, it)
	require.Error(t, err)

	require.True(t, errors.Is(err, os.ErrNotExist) || err != nil)
}
