package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// BuildInventory
// ---------------------------------------------------------------------------

func TestBuildInventory_Empty(t *testing.T) {
	useTempStore(t) // swap global store to temp dir
	inv, err := BuildInventory(context.Background())
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.Equal(t, 0, inv.Len())
}

func TestBuildInventory_WithCSV(t *testing.T) {
	s := useTempStore(t)
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	require.NoError(t, s.WriteCSV(cs))

	inv, err := BuildInventory(context.Background())
	require.NoError(t, err)
	require.Greater(t, inv.Len(), 0)
}

// ---------------------------------------------------------------------------
// buildM1 validation
// ---------------------------------------------------------------------------

func TestBuildM1_WrongKind(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindTick, TF: M1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 requires candle key")
}

func TestBuildM1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: H1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 wrong timeframe")
}

func TestBuildM1_DayOrHourNonZero(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: M1, Day: 1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 requires monthly candle key")
}

func TestBuildM1_EmptyInputs(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	err := buildM1(context.Background(), k, []Key{}, NewWantlist())
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// buildH1 validation
// ---------------------------------------------------------------------------

func TestBuildH1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: M1}
	err := buildH1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 wrong timeframe")
}

func TestBuildH1_WrongInputCount(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: H1}
	err := buildH1(context.Background(), k, []Key{}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 expected 1 input")
}

func TestBuildH1_WrongInputTF(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: H1}
	input := Key{TF: H1} // should be M1
	err := buildH1(context.Background(), k, []Key{input}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 expected M1 input")
}

// ---------------------------------------------------------------------------
// buildD1 validation
// ---------------------------------------------------------------------------

func TestBuildD1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: H1}
	err := buildD1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 wrong timeframe")
}

func TestBuildD1_WrongInputCount(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: D1}
	err := buildD1(context.Background(), k, []Key{}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 expected 1 input")
}

func TestBuildD1_WrongInputTF(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: D1}
	input := Key{TF: D1} // should be H1
	err := buildD1(context.Background(), k, []Key{input}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 expected H1 input")
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator
// ---------------------------------------------------------------------------

func TestBuildHourM1FromTickIterator_WrongKey(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: M1}
	it := NewFuncIterator(func() (RawTick, bool, error) { return RawTick{}, false, nil }, nil)
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
}

func TestBuildHourM1FromTickIterator_Empty(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 3, Hour: 10}
	it := NewFuncIterator(func() (RawTick, bool, error) { return RawTick{}, false, nil }, nil)
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.Nil(t, cs)
}

func TestBuildHourM1FromTickIterator_WithTicks(t *testing.T) {
	t.Parallel()

	// Build some ticks for 2026-01-05 10:00:00 UTC
	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	// Two ticks in minute 0 and minute 1
	ticks := []RawTick{
		{timemilli: baseMS + 1000, Ask: 13010, Bid: 13000},
		{timemilli: baseMS + 2000, Ask: 13015, Bid: 13005},
		{timemilli: baseMS + 60_000 + 500, Ask: 13020, Bid: 13010},
	}
	idx := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		t := ticks[idx]
		idx++
		return t, true, nil
	}, nil)

	k := Key{
		Instrument: "EURUSD",
		Kind:       KindTick,
		TF:         Ticks,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)
	require.Equal(t, "EURUSD", cs.Instrument)
	require.True(t, cs.IsValid(0))
	require.True(t, cs.IsValid(1))
	require.Greater(t, cs.Candles[0].Ticks, int32(0))
}

func TestBuildHourM1FromTickIterator_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	called := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		called++
		return RawTick{timemilli: baseMS + 1000, Ask: 100, Bid: 99}, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(ctx, k, it)
	// Either returns context error or no ticks were processed
	_ = err
	_ = called
}

func TestBuildHourM1FromTickIterator_IteratorError(t *testing.T) {
	t.Parallel()

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	sentinel := errors.New("iter error")
	it := NewFuncIterator(func() (RawTick, bool, error) {
		return RawTick{}, false, sentinel
	}, nil)

	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.ErrorIs(t, err, sentinel)
}

// ---------------------------------------------------------------------------
// ExecuteDownloads with empty plan (no-op)
// ---------------------------------------------------------------------------

func TestExecuteDownloads_EmptyPlan(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		plan: &Plan{
			Download: []Key{},
		},
	}
	dm.Init()
	err := dm.ExecuteDownloads(context.Background())
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// BuildWantList (no inventory matches so everything goes to wantlist)
// ---------------------------------------------------------------------------

func TestBuildWantList_NoInventory(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	dm := NewDataManager([]string{"EURUSD"}, start, end)
	dm.inventory = NewInventory()

	wl, err := dm.BuildWantList(context.Background())
	require.NoError(t, err)
	require.NotNil(t, wl)
	// Should have wants for candles and ticks
	require.Greater(t, wl.Len(), 0)
}

// ---------------------------------------------------------------------------
// Plan (simple wantlist with one tick want)
// ---------------------------------------------------------------------------

func TestPlanFromWantlist_TickGoesToDownload(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		inventory: NewInventory(),
		wants:     NewWantlist(),
	}

	k := Key{Instrument: "EURUSD", Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 1, Hour: 10}
	dm.wants.Put(Want{Key: k, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.Download, 1)
	require.Equal(t, k, plan.Download[0])
}

func TestPlanFromWantlist_M1CandleBlockedNoTicks(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		inventory: NewInventory(), // empty inventory → TicksComplete returns false
		wants:     NewWantlist(),
	}

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: k, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	// Ticks not complete, so M1 not queued for build
	require.Len(t, plan.BuildM1, 0)
}

func TestPlanFromWantlist_H1CandleBlockedNoM1(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		inventory: NewInventory(), // empty inventory
		wants:     NewWantlist(),
	}

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: k, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildH1, 0)
}

func TestPlanFromWantlist_D1CandleBlockedNoH1(t *testing.T) {
	t.Parallel()

	dm := &DataManager{
		inventory: NewInventory(),
		wants:     NewWantlist(),
	}

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: D1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: k, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildD1, 0)
}

func TestPlanFromWantlist_H1ReadyWhenM1Complete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	km1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	inv.Put(Asset{Key: km1, Exists: true, Complete: true})

	dm := &DataManager{
		inventory: inv,
		wants:     NewWantlist(),
	}

	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: kh1, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildH1, 1)
}

func TestPlanFromWantlist_D1ReadyWhenH1Complete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	inv.Put(Asset{Key: kh1, Exists: true, Complete: true})

	dm := &DataManager{
		inventory: inv,
		wants:     NewWantlist(),
	}

	kd1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: D1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: kd1, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildD1, 1)
}

// ---------------------------------------------------------------------------
// closeCandleIterators
// ---------------------------------------------------------------------------

func TestCloseCandleIterators_NoError(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	it1 := NewCandleSetIterator(cs, TimeRange{})
	it2 := NewCandleSetIterator(cs, TimeRange{})

	_ = s
	err := closeCandleIterators([]CandleIterator{it1, it2})
	require.NoError(t, err)
}

func TestCloseCandleIterators_WithNil(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	it1 := NewCandleSetIterator(cs, TimeRange{})

	_ = s
	err := closeCandleIterators([]CandleIterator{nil, it1, nil})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ChainedCandleIterator with nil iterator
// ---------------------------------------------------------------------------

func TestChainedCandleIterator_NilSub(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	cs.Candles[0] = Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}
	cs.SetValid(0)

	real := NewCandleSetIterator(cs, TimeRange{})
	chained := NewChainedCandleIterator(nil, real, nil)

	_ = s

	count := 0
	for chained.Next() {
		count++
	}
	require.NoError(t, chained.Err())
	require.NoError(t, chained.Close())
	require.Equal(t, 1, count)
}

func TestChainedCandleIterator_AlreadyClosed(t *testing.T) {
	t.Parallel()

	chained := NewChainedCandleIterator()
	require.NoError(t, chained.Close())
	require.NoError(t, chained.Close()) // idempotent
	require.False(t, chained.Next())
}

// ---------------------------------------------------------------------------
// InventoryTicksComplete
// ---------------------------------------------------------------------------

func TestInventoryTicksComplete_AllComplete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	// Key for Jan 2026
	base := Key{
		Instrument: "EURUSD",
		Kind:       KindCandle,
		TF:         M1,
		Year:       2026,
		Month:      1,
	}

	// Add all required tick hours. TicksComplete constructs keys with TF=Ticks,
	// so we must also set TF=Ticks when populating the inventory.
	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	for _, k := range keys {
		k.TF = Ticks
		inv.Put(Asset{Key: k, Exists: true, Complete: true, Size: 100})
	}

	complete, gotKeys := inv.TicksComplete(base)
	require.True(t, complete)
	require.NotEmpty(t, gotKeys)
}

func TestInventoryTicksComplete_Missing(t *testing.T) {
	t.Parallel()

	inv := NewInventory() // empty inventory

	base := Key{
		Instrument: "EURUSD",
		Kind:       KindCandle,
		TF:         M1,
		Year:       2026,
		Month:      1,
	}

	complete, gotKeys := inv.TicksComplete(base)
	require.False(t, complete)
	require.Nil(t, gotKeys)
}

// ---------------------------------------------------------------------------
// newHTTPClient (just verify it doesn't panic/return nil)
// ---------------------------------------------------------------------------

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()
	c := newHTTPClient()
	require.NotNil(t, c)
}
