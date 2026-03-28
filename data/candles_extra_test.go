package data

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DataManager.Candles validation
// ---------------------------------------------------------------------------

func TestCandles_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := &DataManager{}
	_, err := dm.Candles(ctx, CandleRequest{
		Instrument: "EURUSD",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   types.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
		},
	})
	require.Error(t, err)
}

func TestCandles_BlankInstrument(t *testing.T) {
	t.Parallel()

	dm := &DataManager{}
	_, err := dm.Candles(context.Background(), CandleRequest{
		Instrument: "",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   types.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
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
		Timeframe:  types.Ticks, // unsupported
		Range: types.TimeRange{
			Start: types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   types.FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
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
		Timeframe:  types.H1,
		Range:      types.TimeRange{}, // zero range is invalid
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid candle range")
}

func TestCandles_DefaultSourceFallsBackToCandles(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	writeMonthlyCandles(t, s, "EURUSD", types.H1, 2026, time.January, nil)

	dm := &DataManager{}
	req := CandleRequest{
		Source:     "", // empty source should fall back to SourceCandles
		Instrument: "EURUSD",
		Timeframe:  types.H1,
		Range: types.TimeRange{
			Start: types.FromTime(start),
			End:   types.FromTime(end),
		},
		Strict: false,
	}
	it, err := dm.Candles(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, it)
	require.NoError(t, it.Close())
}

// ---------------------------------------------------------------------------
// loadCandleSet with cancelled context
// ---------------------------------------------------------------------------

func TestLoadCandleSet_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := &DataManager{}
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	_, err := dm.loadCandleSet(ctx, k)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// buildM1 with bad tick key in inputs
// ---------------------------------------------------------------------------

func TestBuildM1_BadTickKey(t *testing.T) {
	s := useTempStore(t)
	_ = s

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	// input key is candle, not tick
	badInput := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	err := buildM1(context.Background(), k, []Key{badInput}, NewWantlist())
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// candleMaker with empty plan
// ---------------------------------------------------------------------------

func TestCandleMaker_EmptyPlan(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		plan: &Plan{
			BuildM1: []BuildTask{},
			BuildH1: []BuildTask{},
			BuildD1: []BuildTask{},
		},
		wants: NewWantlist(),
	}
	err := dm.candleMaker(context.Background())
	require.NoError(t, err)
}
