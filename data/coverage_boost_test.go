package data

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// candleMaker with H1 and D1 tasks that fail early
// ---------------------------------------------------------------------------

func TestCandleMaker_H1TaskFails(t *testing.T) {
	s := useTempStore(t)
	_ = s

	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	// Input M1 key doesn't exist in store → buildH1 will fail on ReadCSV
	km1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}

	dm := &DataManager{
		plan: &Plan{
			BuildM1: []BuildTask{},
			BuildH1: []BuildTask{
				{Key: kh1, Inputs: []Key{km1}, Kind: BuildH1},
			},
			BuildD1: []BuildTask{},
		},
		wants: NewWantlist(),
	}
	err := dm.candleMaker(context.Background())
	require.Error(t, err)
}

func TestCandleMaker_D1TaskFails(t *testing.T) {
	s := useTempStore(t)
	_ = s

	kd1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.D1, Year: 2026, Month: 1}
	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}

	dm := &DataManager{
		plan: &Plan{
			BuildM1: []BuildTask{},
			BuildH1: []BuildTask{},
			BuildD1: []BuildTask{
				{Key: kd1, Inputs: []Key{kh1}, Kind: BuildD1},
			},
		},
		wants: NewWantlist(),
	}
	err := dm.candleMaker(context.Background())
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator with gap → exercises fillFlat path
// ---------------------------------------------------------------------------

func TestBuildHourM1FromTickIterator_WithGap(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	// Tick at minute 0 and tick at minute 5 (gap of 4 minutes)
	ticks := []Tick{
		{Timemilli: baseMS + 1000, Ask: 13010, Bid: 13000},
		{Timemilli: baseMS + 5*60_000 + 500, Ask: 13020, Bid: 13010},
	}
	idx := 0
	it := NewFuncIterator(func() (Tick, bool, error) {
		if idx >= len(ticks) {
			return Tick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{
		Instrument: "EURUSD",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)
	// Minute 0 should be valid (actual tick data)
	require.True(t, cs.IsValid(0))
	// Minute 5 should be valid (actual tick data)
	require.True(t, cs.IsValid(5))
	// Minute 1-4 should have flat placeholders (not valid, filled in)
	require.False(t, cs.IsValid(1))
}

// ---------------------------------------------------------------------------
// ExecuteDownloads: context cancelled while draining queue
// ---------------------------------------------------------------------------

func TestExecuteDownloads_ContextCancelled(t *testing.T) {
	s := useTempStore(t)
	_ = s

	k := Key{
		Kind:       KindTick,
		TF:         types.Ticks,
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}

	// Cancel context immediately so downloads stop right away.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := &DataManager{
		inventory: NewInventory(),
		plan: &Plan{
			Download: []Key{k},
		},
	}
	dm.Init()

	err := dm.ExecuteDownloads(ctx)
	require.NoError(t, err)
}
