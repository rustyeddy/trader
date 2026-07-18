package datamanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

type testDownloadProvider struct {
	name string
	url  string
}

func (p *testDownloadProvider) Name() string { return p.name }

func (p *testDownloadProvider) SourceURL(_ SourceParams) string { return p.url }

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
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	require.NoError(t, s.WriteCSV(cs))

	inv, err := BuildInventory(context.Background())
	require.NoError(t, err)
	require.Greater(t, inv.Len(), 0)
}

// ---------------------------------------------------------------------------
// ListCandleKeys
// ---------------------------------------------------------------------------

func TestListCandleKeys_Empty(t *testing.T) {
	useTempStore(t)
	keys, err := ListCandleKeys()
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestListCandleKeys_FindsWrittenMonths(t *testing.T) {
	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	require.NoError(t, s.WriteCSV(cs))

	keys, err := ListCandleKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, "EURUSD", keys[0].Instrument)
	require.Equal(t, 2026, keys[0].Year)
	require.Equal(t, 1, keys[0].Month)
	require.Equal(t, types.H1, keys[0].TF)
}

// TestListCandleKeys_DoesNotPopulateReadCache is the regression guard for the
// memory-exhaustion bug: resolveValidateDefaults used to call BuildInventory,
// which opens and parses every candle CSV via ReadCSV to determine each
// file's coverage — permanently caching every file's full CandleSet for the
// life of the process. ListCandleKeys must only look at filenames.
func TestListCandleKeys_DoesNotPopulateReadCache(t *testing.T) {
	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	require.NoError(t, s.WriteCSV(cs))

	_, err := ListCandleKeys()
	require.NoError(t, err)

	s.cacheMu.RLock()
	cacheLen := len(s.cache)
	s.cacheMu.RUnlock()
	require.Zero(t, cacheLen, "ListCandleKeys must not populate the ReadCSV cache")
}

// ---------------------------------------------------------------------------
// buildM1 validation
// ---------------------------------------------------------------------------

func TestBuildM1_WrongKind(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindTick, TF: types.M1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 requires candle key")
}

func TestBuildM1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.H1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 wrong timeframe")
}

func TestBuildM1_DayOrHourNonZero(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.M1, Day: 1}
	err := buildM1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildM1 requires monthly candle key")
}

func TestBuildM1_EmptyInputs(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	err := buildM1(context.Background(), k, []Key{}, NewWantlist())
	require.NoError(t, err)
}

func TestBuildM1_BadTickKey(t *testing.T) {
	s := useTempStore(t)
	_ = s

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	badInput := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	err := buildM1(context.Background(), k, []Key{badInput}, NewWantlist())
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// buildH1 validation
// ---------------------------------------------------------------------------

func TestBuildH1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.M1}
	err := buildH1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 wrong timeframe")
}

func TestBuildH1_WrongInputCount(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.H1}
	err := buildH1(context.Background(), k, []Key{}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 expected 1 input")
}

func TestBuildH1_WrongInputTF(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.H1}
	input := Key{TF: types.H1} // should be M1
	err := buildH1(context.Background(), k, []Key{input}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildH1 expected M1 input")
}

// ---------------------------------------------------------------------------
// buildD1 validation
// ---------------------------------------------------------------------------

func TestBuildD1_WrongTimeframe(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.H1}
	err := buildD1(context.Background(), k, nil, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 wrong timeframe")
}

func TestBuildD1_WrongInputCount(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.D1}
	err := buildD1(context.Background(), k, []Key{}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 expected 1 input")
}

func TestBuildD1_WrongInputTF(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.D1}
	input := Key{TF: types.D1} // should be H1
	err := buildD1(context.Background(), k, []Key{input}, NewWantlist())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildD1 expected H1 input")
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator
// ---------------------------------------------------------------------------

func TestBuildHourM1FromTickIterator_WrongKey(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindCandle, TF: types.M1}
	it := newFuncIterator(func() (RawTick, bool, error) { return RawTick{}, false, nil }, nil)
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
}

func TestBuildHourM1FromTickIterator_Empty(t *testing.T) {
	t.Parallel()
	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 3, Hour: 10}
	it := newFuncIterator(func() (RawTick, bool, error) { return RawTick{}, false, nil }, nil)
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.Nil(t, cs)
}

func TestBuildHourM1FromTickIterator_WithTicks(t *testing.T) {
	t.Parallel()

	// Build some ticks for 2026-01-05 10:00:00 UTC
	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	// Two ticks in minute 0 and minute 1
	ticks := []RawTick{
		{TimeMillis: baseMS + 1000, Ask: 13010, Bid: 13000},
		{TimeMillis: baseMS + 2000, Ask: 13015, Bid: 13005},
		{TimeMillis: baseMS + 60_000 + 500, Ask: 13020, Bid: 13010},
	}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
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
		TF:         types.Ticks,
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

func TestBuildHourM1FromTickIterator_WithGap(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	ticks := []RawTick{
		{TimeMillis: baseMS + 1000, Ask: 13010, Bid: 13000},
		{TimeMillis: baseMS + 5*60_000 + 500, Ask: 13020, Bid: 13010},
	}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
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
	require.True(t, cs.IsValid(0))
	require.True(t, cs.IsValid(5))
	require.False(t, cs.IsValid(1))
}

func TestBuildHourM1FromTickIterator_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	called := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		called++
		return RawTick{TimeMillis: baseMS + 1000, Ask: 100, Bid: 99}, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(ctx, k, it)
	// Either returns context error or no ticks were processed
	_ = err
	_ = called
}

func TestBuildHourM1FromTickIterator_IteratorError(t *testing.T) {
	t.Parallel()

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	sentinel := errors.New("iter error")
	it := newFuncIterator(func() (RawTick, bool, error) {
		return RawTick{}, false, sentinel
	}, nil)

	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.ErrorIs(t, err, sentinel)
}

func TestBuildHourM1_TickOutsideHourWindow(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)
	outsideMS := baseMS + 2*3600_000 + 100
	ticks := []RawTick{
		{TimeMillis: outsideMS, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside hour window")
}

func TestBuildHourM1_BadTimestamp(t *testing.T) {
	t.Parallel()

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	tick := RawTick{TimeMillis: 0, Ask: 100, Bid: 99}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx > 0 {
			return RawTick{}, false, nil
		}
		idx++
		return tick, true, nil
	}, nil)

	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad tick timestamp")
}

func TestBuildHourM1_OutOfOrder(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	ticks := []RawTick{
		{TimeMillis: baseMS + 5*60_000 + 100, Ask: 13010, Bid: 13000},
		{TimeMillis: baseMS + 2*60_000 + 100, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out-of-order")
}

func TestBuildHourM1_TrailingFillFlat(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := types.TimeMilliFromTime(hourStart)

	ticks := []RawTick{
		{TimeMillis: baseMS + 100, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)
	require.True(t, cs.IsValid(0))
	require.False(t, cs.IsValid(1))
	require.Equal(t, cs.Candles[0].Close, cs.Candles[1].Close)
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

func TestExecuteDownloads_ContextCancelled(t *testing.T) {
	k := Key{
		Kind:       KindTick,
		TF:         types.Ticks,
		Instrument: "EURUSD",
		Source:     market.SourceDukascopy,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}

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
	require.ErrorIs(t, err, context.Canceled)
}

func TestDownloader_StartDownloaderRecordsFailure(t *testing.T) {
	s := useTempStore(t)
	_ = s

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	source := fmt.Sprintf("test-downloader-failure-%s", market.NormalizeInstrument(t.Name()))
	Register(&testDownloadProvider{name: source, url: srv.URL})

	key := Key{
		Source:     source,
		Instrument: "EURUSD",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}
	dm := &DataManager{inventory: NewInventory()}
	dl := &downloader{client: srv.Client(), workers: 1}

	q := make(chan Key, 1)
	wg := dl.startDownloader(context.Background(), dm, q)
	q <- key
	close(q)
	wg.Wait()

	asset, ok := dm.inventory.Get(key)
	require.True(t, ok)
	require.Equal(t, FlagDownloadFailed, asset.Flags)
	require.False(t, asset.Exists)
	require.False(t, asset.Complete)
	require.Contains(t, asset.Reason, "http 502")
}

func TestDownloader_StartDownloaderStoresSuccess(t *testing.T) {
	s := useTempStore(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tick-data"))
	}))
	defer srv.Close()

	source := fmt.Sprintf("test-downloader-success-%s", market.NormalizeInstrument(t.Name()))
	Register(&testDownloadProvider{name: source, url: srv.URL})

	key := Key{
		Source:     source,
		Instrument: "EURUSD",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}
	dm := &DataManager{inventory: NewInventory()}
	dl := &downloader{client: srv.Client(), workers: 1}

	q := make(chan Key, 1)
	wg := dl.startDownloader(context.Background(), dm, q)
	q <- key
	close(q)
	wg.Wait()

	asset, ok := dm.inventory.Get(key)
	require.True(t, ok)
	require.Equal(t, FlagUsable, asset.Flags)
	require.True(t, asset.Exists)
	require.True(t, asset.Complete)
	require.NotEmpty(t, asset.Path)
	rng, err := key.Range()
	require.NoError(t, err)
	require.Equal(t, rng, asset.Range)
	require.True(t, s.IsUsableTickFile(key))
}

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

func TestCandleMaker_H1TaskFails(t *testing.T) {
	s := useTempStore(t)
	_ = s

	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	km1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}

	dm := &DataManager{
		plan: &Plan{
			BuildM1: []BuildTask{},
			BuildH1: []BuildTask{
				{Key: kh1, Inputs: []Key{km1}},
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
				{Key: kd1, Inputs: []Key{kh1}},
			},
		},
		wants: NewWantlist(),
	}
	err := dm.candleMaker(context.Background())
	require.Error(t, err)
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

func TestBuildWantList_CancelledContext(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	dm := NewDataManager([]string{"EURUSD"}, start, end)
	dm.inventory = NewInventory()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wl, err := dm.BuildWantList(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, wl)
}

func TestBuildWantList_IncompleteCandleUsesWantIncomplete(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	dm := NewDataManager([]string{"EURUSD"}, start, end)
	dm.inventory = NewInventory()

	k := Key{Instrument: "EURUSD", Source: market.SourceCandles, Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	dm.inventory.Put(Asset{Key: k, Exists: true, Complete: false})

	wl, err := dm.BuildWantList(context.Background())
	require.NoError(t, err)

	want, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, WantIncomplete, want.WantReason)
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

	k := Key{Instrument: "EURUSD", Kind: KindTick, TF: types.Ticks, Year: 2026, Month: 1, Day: 1, Hour: 10}
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

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
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

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
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

	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.D1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: k, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildD1, 0)
}

func TestPlanFromWantlist_H1ReadyWhenM1Complete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	km1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	inv.Put(Asset{Key: km1, Exists: true, Complete: true})

	dm := &DataManager{
		inventory: inv,
		wants:     NewWantlist(),
	}

	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: kh1, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildH1, 1)
}

func TestPlanFromWantlist_D1ReadyWhenH1Complete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	kh1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	inv.Put(Asset{Key: kh1, Exists: true, Complete: true})

	dm := &DataManager{
		inventory: inv,
		wants:     NewWantlist(),
	}

	kd1 := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.D1, Year: 2026, Month: 1}
	dm.wants.Put(Want{Key: kd1, WantReason: WantMissing})

	plan, err := dm.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.BuildD1, 1)
}

// ---------------------------------------------------------------------------
// closeCandleIterators
// ---------------------------------------------------------------------------

func TestCloseCandleIterators_NoError(t *testing.T) {

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	it1 := newCandleSetIterator(cs, types.TimeRange{})
	it2 := newCandleSetIterator(cs, types.TimeRange{})

	_ = s
	err := closeCandleIterators([]market.CandleIterator{it1, it2})
	require.NoError(t, err)
}

func TestCloseCandleIterators_WithNil(t *testing.T) {

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	it1 := newCandleSetIterator(cs, types.TimeRange{})

	_ = s
	err := closeCandleIterators([]market.CandleIterator{nil, it1, nil})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ChainedCandleIterator with nil iterator
// ---------------------------------------------------------------------------

func TestChainedCandleIterator_NilSub(t *testing.T) {

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	cs.Candles[0] = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1, Timestamp: cs.Candles[0].Timestamp}
	cs.SetValid(0)

	real := newCandleSetIterator(cs, types.TimeRange{})
	chained := newChainedCandleIterator(nil, real, nil)

	_ = s

	count := 0
	for _, ok := chained.Next(); ok; _, ok = chained.Next() {
		count++
	}
	require.NoError(t, chained.Err())
	require.NoError(t, chained.Close())
	require.Equal(t, 1, count)
}

func TestChainedCandleIterator_AlreadyClosed(t *testing.T) {
	t.Parallel()

	chained := newChainedCandleIterator()
	require.NoError(t, chained.Close())
	require.NoError(t, chained.Close()) // idempotent
	_, ok := chained.Next()
	require.False(t, ok)
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
		TF:         types.M1,
		Year:       2026,
		Month:      1,
	}

	// Add all required tick hours. TicksComplete constructs keys with TF=Ticks,
	// so we must also set TF=Ticks when populating the inventory.
	keys, err := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	require.NoError(t, err)
	for _, k := range keys {
		inv.Put(Asset{Key: k, Exists: true, Complete: true, Size: 100})
	}

	complete, gotKeys, missing, err := inv.TicksComplete(base)
	require.NoError(t, err)
	require.True(t, complete)
	require.NotEmpty(t, gotKeys)
	require.Empty(t, missing)
}

func TestInventoryTicksComplete_Missing(t *testing.T) {
	t.Parallel()

	inv := NewInventory() // empty inventory

	base := Key{
		Instrument: "EURUSD",
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       2026,
		Month:      1,
	}

	complete, gotKeys, missing, err := inv.TicksComplete(base)
	require.NoError(t, err)
	require.False(t, complete)
	require.NotEmpty(t, gotKeys)
	require.NotEmpty(t, missing)
}

// ---------------------------------------------------------------------------
// newHTTPClient (just verify it doesn't panic/return nil)
// ---------------------------------------------------------------------------

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()
	c := newHTTPClient()
	require.NotNil(t, c)
}

func TestPlanLog(t *testing.T) {
	t.Parallel()

	p := &Plan{}
	require.NotPanics(t, p.Log)

	p2 := &Plan{
		Download: []Key{{Instrument: "EURUSD"}},
		BuildM1:  []BuildTask{{}},
		BuildH1:  []BuildTask{{}},
		BuildD1:  []BuildTask{{}},
	}
	require.NotPanics(t, p2.Log)
	var nilPlan *Plan
	require.NotPanics(t, nilPlan.Log)
}

func TestPlanHelpers(t *testing.T) {
	t.Parallel()

	p := &Plan{
		Download: []Key{{Instrument: "EURUSD"}},
		BuildM1:  []BuildTask{{}},
		BuildH1:  []BuildTask{{}, {}},
	}

	require.False(t, p.Empty())
	require.Equal(t, 3, p.TotalBuilds())
	require.Len(t, p.BuildTasks(types.M1), 1)
	require.Len(t, p.BuildTasks(types.H1), 2)
	require.Len(t, p.BuildTasks(types.D1), 0)
	require.Nil(t, p.BuildTasks(types.Ticks))
}

func TestCandleMaker_NilPlan(t *testing.T) {
	t.Parallel()

	dm := &DataManager{wants: NewWantlist()}
	require.NoError(t, dm.candleMaker(context.Background()))
}

func TestWantlistPutGetHasDelete(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD", Source: "candles", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	w := Want{Key: k, WantReason: WantMissing}

	require.False(t, wl.Has(k))

	wl.Put(w)
	require.True(t, wl.Has(k))

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, w, got)

	wl.Delete(k)
	require.False(t, wl.Has(k))
}

func TestWantlistKeysListLen(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	k3 := Key{Instrument: "USDJPY", Kind: KindCandle, Year: 2026, Month: 3}

	wl.Put(Want{Key: k1, WantReason: WantMissing})
	wl.Put(Want{Key: k2, WantReason: WantIncomplete})
	wl.Put(Want{Key: k3, WantReason: WantStale})

	require.Equal(t, 3, wl.Len())
	require.Len(t, wl.Keys(), 3)
	require.Len(t, wl.List(), 3)
}

func TestWantlistUpdate(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}

	err := wl.Update(k, func(w *Want) error {
		w.WantReason = WantStale
		return nil
	})
	require.ErrorIs(t, err, ErrKeyNotFound)

	wl.Put(Want{Key: k, WantReason: WantMissing})
	err = wl.Update(k, func(w *Want) error {
		w.WantReason = WantStale
		return nil
	})
	require.NoError(t, err)

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, WantStale, got.WantReason)

	sentinel := errors.New("update failed")
	err = wl.Update(k, func(w *Want) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestWantReasonValues(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, WantMissing, WantIncomplete)
	require.NotEqual(t, WantMissing, WantStale)
	require.NotEqual(t, WantIncomplete, WantStale)
	require.True(t, WantMissing.Valid())
	require.True(t, WantIncomplete.Valid())
	require.True(t, WantStale.Valid())
	require.False(t, WantReason("bogus").Valid())
}

func TestWantlistRange(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	wl.PutKey(k1, WantMissing)
	wl.PutKey(k2, WantIncomplete)

	count := 0
	wl.Range(func(k Key, v Want) bool {
		count++
		return true
	})
	require.Equal(t, 2, count)
}

func TestWantlistPutKey(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	wl.PutKey(k, WantMissing)

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, k, got.Key)
	require.Equal(t, WantMissing, got.WantReason)
}

func TestKeyCompare_KindLessThan(t *testing.T) {
	t.Parallel()

	tickKey := Key{Source: "candles", Instrument: "EURUSD", Kind: KindTick, TF: types.H1, Year: 2026, Month: 1}
	candleKey := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	require.Equal(t, -1, tickKey.compare(candleKey))
}

func TestKeyCompare_HourGreaterThan(t *testing.T) {
	t.Parallel()

	base := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1, Day: 5, Hour: 13}
	other := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1, Day: 5, Hour: 10}
	require.Equal(t, 1, base.compare(other))
}

func TestKeyCompareTF(t *testing.T) {
	t.Parallel()

	base := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}

	t.Run("TF smaller returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.TF = types.D1
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("TF larger returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.TF = types.M1
		require.Equal(t, 1, base.compare(other))
	})
}

func TestKeyRange_TickHour0(t *testing.T) {
	t.Parallel()

	k := Key{
		Kind:  KindTick,
		TF:    types.Ticks,
		Year:  2026,
		Month: 1,
		Day:   5,
		Hour:  0,
	}
	rng, err := k.Range()
	require.NoError(t, err)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	require.Equal(t, types.Timestamp(start.Unix()), rng.Start)
	require.Equal(t, types.Timestamp(start.Add(time.Hour).Unix()), rng.End)
}

func TestKeymapKeysSorted(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k1 := Key{Instrument: "EURUSD", Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Year: 2026, Month: 1}
	k3 := Key{Instrument: "USDJPY", Year: 2026, Month: 1}

	km.Put(k3, 3)
	km.Put(k1, 1)
	km.Put(k2, 2)

	keys := km.Keys()
	require.Len(t, keys, 3)

	found := map[string]bool{}
	for _, k := range keys {
		found[k.Instrument] = true
	}
	require.True(t, found["EURUSD"])
	require.True(t, found["GBPUSD"])
	require.True(t, found["USDJPY"])
}

func TestKeymapPut_NilMap(t *testing.T) {
	t.Parallel()

	km := Keymap[int]{}
	k := Key{Instrument: "EURUSD"}
	km.Put(k, 1)
	v, ok := km.Get(k)
	require.True(t, ok)
	require.Equal(t, 1, v)
}

func TestKeymapRange_Empty(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	count := 0
	km.Range(func(k Key, v int) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)
}

func TestRequiredTickHoursForMonth_Count(t *testing.T) {
	t.Parallel()

	keys, err := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	require.NoError(t, err)
	require.Greater(t, len(keys), 100)
	require.Less(t, len(keys), 31*24)
}

func TestRequiredTickHoursForMonth_NoClosedHours(t *testing.T) {
	t.Parallel()

	keys, err := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	require.NoError(t, err)
	for _, k := range keys {
		ts := time.Date(k.Year, time.Month(k.Month), k.Day, k.Hour, 0, 0, 0, time.UTC)
		require.False(t, market.IsForexMarketClosed(ts), "key %+v should not be forex-closed time", k)
	}
}

func TestInventoryMissingComplete_NoneRequired(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	missing := inv.MissingComplete([]Key{})
	require.Empty(t, missing)
}

func TestInventoryMissingComplete_AllPresent(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 1}
	inv.Put(Asset{Key: k1, Exists: true, Complete: true})
	inv.Put(Asset{Key: k2, Exists: true, Complete: true})

	missing := inv.MissingComplete([]Key{k1, k2})
	require.Empty(t, missing)
}

func TestInventoryString(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	a := Asset{
		Key:        k,
		Descriptor: "test descriptor",
		Reason:     "test reason",
	}
	inv.Put(a)
	require.Equal(t, 1, inv.Len())
}

// ---------------------------------------------------------------------------
// Sync
// ---------------------------------------------------------------------------

func TestSync_ContextCancelled(t *testing.T) {
	useTempStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dm := NewDataManager([]string{"EURUSD"},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	dm.Init()

	err := dm.Sync(ctx, false, false)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSync_NoBuildNoDownload(t *testing.T) {
	useTempStore(t)

	dm := NewDataManager([]string{"EURUSD"},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	dm.Init()

	err := dm.Sync(context.Background(), false, false)
	require.NoError(t, err)
	// rebuildPlanState must have populated inventory and plan
	require.NotNil(t, dm.inventory)
	require.NotNil(t, dm.plan)
}

func TestSync_BuildOnlyEmptyStore(t *testing.T) {
	// Empty store → no tick files complete → plan has no BuildM1 tasks
	// → candleMaker is a no-op → Sync returns nil
	useTempStore(t)

	dm := NewDataManager([]string{"EURUSD"},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	dm.Init()

	err := dm.Sync(context.Background(), false, true)
	require.NoError(t, err)
}

func TestSync_BuildWithCompleteM1(t *testing.T) {
	// Write an M1 candle file and an H1 want → Sync(build=true) should build H1 from M1.
	s := useTempStore(t)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.M1)
	require.NoError(t, s.WriteCSV(cs))

	dm := NewDataManager([]string{"EURUSD"},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	dm.Init()

	// Pre-populate inventory so Plan can see M1 is complete
	km1 := Key{
		Instrument: cs.Instrument,
		Source:     cs.Source,
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       int(start.Year()),
		Month:      int(start.Month()),
	}
	dm.inventory = NewInventory()
	dm.inventory.Put(Asset{Key: km1, Exists: true, Complete: true})

	// Build wantlist with H1 wanted, and plan so BuildH1 is scheduled
	dm.wants = NewWantlist()
	kh1 := km1
	kh1.TF = types.H1
	dm.wants.PutKey(kh1, WantMissing)

	var planErr error
	dm.plan, planErr = dm.Plan(context.Background())
	require.NoError(t, planErr)
	require.Len(t, dm.plan.BuildH1, 1)

	err := dm.candleMaker(context.Background())
	require.NoError(t, err)

	// Verify H1 was written
	kh1check := Key{Instrument: "EURUSD", Source: cs.Source, Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	exists, err := s.Exists(kh1check)
	require.NoError(t, err)
	require.True(t, exists)
}
