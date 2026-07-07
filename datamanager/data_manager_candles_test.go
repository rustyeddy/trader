package datamanager

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

func useTempStore(t *testing.T) *store {
	t.Helper()

	s := newStoreAt(t.TempDir())
	restore := swapStore(s)
	t.Cleanup(restore)

	return s
}

func writeMonthlyCandles(
	t *testing.T,
	s *store,
	instrument string,
	tf market.Timeframe,
	year int,
	month time.Month,
	rows map[time.Time]market.Candle,
) Key {
	t.Helper()

	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)

	cs, err := newMonthlyCandleSet(
		instrument,
		tf,
		market.FromTime(start),
		market.PriceScale,
		market.SourceCandles,
	)
	require.NoError(t, err)

	for ts, c := range rows {
		require.NoError(t, cs.AddCandle(market.FromTime(ts.UTC()), c))
	}

	require.NoError(t, s.WriteCSV(cs))

	return Key{
		Instrument: instrument,
		Source:     market.SourceCandles,
		Kind:       KindCandle,
		TF:         tf,
		Year:       year,
		Month:      int(month),
	}
}

func collectCandles(t *testing.T, it market.CandleIterator) ([]market.Timestamp, []market.Candle) {
	t.Helper()

	var outTS []market.Timestamp
	var outCandles []market.Candle

	for ct, ok := it.Next(); ok; ct, ok = it.Next() {
		outTS = append(outTS, ct.Timestamp)
		outCandles = append(outCandles, ct.Candle)
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

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.January, map[time.Time]market.Candle{
		jan31_23: {
			Open:  101,
			High:  105,
			Low:   100,
			Close: 104,
			Ticks: 10,
		},
	})

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.February, map[time.Time]market.Candle{
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
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(jan31_23),
			End:   market.FromTime(feb01_02), // exclusive
			TF:    market.H1,
		},
		Strict: true,
	}

	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)

	gotTS, gotCandles := collectCandles(t, it)

	times := []market.Timestamp{
		market.FromTime(jan31_23),
		market.FromTime(feb01_00),
		market.FromTime(feb01_01),
	}
	require.Equal(t, times, gotTS)
	require.Equal(t, []market.Candle{
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

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.January, map[time.Time]market.Candle{
		jan15_00: {
			Open:  111,
			High:  112,
			Low:   110,
			Close: 111,
			Ticks: 11,
		},
	})

	req := CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
		Strict: false,
	}

	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)

	gotTS, gotCandles := collectCandles(t, it)

	require.Equal(t, []market.Timestamp{
		market.FromTime(jan15_00),
	}, gotTS)

	require.Equal(t, []market.Candle{
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

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.January, map[time.Time]market.Candle{
		jan15_00: {
			Open:  111,
			High:  112,
			Low:   110,
			Close: 111,
			Ticks: 11,
		},
	})

	req := CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
		Strict: true,
	}

	it, err := dm.Candles(context.Background(), req)
	require.Nil(t, it)
	require.Error(t, err)

	require.True(t, errors.Is(err, os.ErrNotExist) || err != nil)
}

func TestCandles_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := &DataManager{}
	_, err := dm.Candles(ctx, CandleRequest{
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
	})
	require.Error(t, err)
}

func TestCandles_BlankInstrument(t *testing.T) {
	t.Parallel()

	dm := &DataManager{}
	_, err := dm.Candles(context.Background(), CandleRequest{
		Instrument: "",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "blank instrument")
}

func TestCandles_UnsupportedTimeframe(t *testing.T) {
	t.Parallel()

	dm := &DataManager{}
	_, err := dm.Candles(context.Background(), CandleRequest{
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.Ticks,
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported candle timeframe")
}

func TestCandles_InvalidRange(t *testing.T) {
	t.Parallel()

	dm := &DataManager{}
	_, err := dm.Candles(context.Background(), CandleRequest{
		Instrument: "EURUSD",
		Range: market.TimeRange{
			TF: market.H1,
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid candle range")
}

func TestCandles_DefaultSourceFallsBackToCandles(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.January, nil)

	dm := &DataManager{}
	req := CandleRequest{
		Source:     "",
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(start),
			End:   market.FromTime(end),
			TF:    market.H1,
		},
		Strict: false,
	}
	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)
	require.NoError(t, it.Close())
}

func TestCandles_StrictMissingFileWrapsError(t *testing.T) {
	s := useTempStore(t)
	_ = s

	dm := &DataManager{}
	req := CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
		Strict: true,
	}
	_, err := dm.Candles(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load candles")
}

func TestCandles_ContextCancelledDuringIteration(t *testing.T) {
	s := useTempStore(t)

	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.January, nil)
	writeMonthlyCandles(t, s, "EURUSD", market.H1, 2026, time.February, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := &DataManager{}
	req := CandleRequest{
		Source:     market.SourceCandles,
		Instrument: "EURUSD",
		Range: market.TimeRange{
			Start: market.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   market.FromTime(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
			TF:    market.H1,
		},
	}
	_, err := dm.Candles(ctx, req)
	require.Error(t, err)
}
