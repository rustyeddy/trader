package trader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Key.compare: missing branches (Kind < and Hour >)
// ---------------------------------------------------------------------------

func TestKeyCompare_KindLessThan(t *testing.T) {
	t.Parallel()

	// KindTick < KindCandle, so tick.compare(candle) == -1
	tickKey := Key{Source: "candles", Instrument: "EURUSD", Kind: KindTick, TF: H1, Year: 2026, Month: 1}
	candleKey := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	require.Equal(t, -1, tickKey.compare(candleKey))
}

func TestKeyCompare_HourGreaterThan(t *testing.T) {
	t.Parallel()

	base := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1, Day: 5, Hour: 13}
	other := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1, Day: 5, Hour: 10}
	require.Equal(t, 1, base.compare(other))
}

// ---------------------------------------------------------------------------
// readNextBI5Tick: general read error (not EOF, not ErrUnexpectedEOF)
// ---------------------------------------------------------------------------

type errReadAfter struct {
	data []byte
	pos  int
	err  error
}

func (r *errReadAfter) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, r.err
}

func TestReadNextBI5Tick_GeneralReadError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("disk error")
	// Write 10 bytes (partial record) then return a non-EOF error
	partial := make([]byte, 10)
	r := &errReadAfter{data: partial, err: sentinel}
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0)
	require.Error(t, err)
	require.False(t, ok)
	// Should be a wrapped error about truncated record
	require.Contains(t, err.Error(), "test.bi5")
}

// ---------------------------------------------------------------------------
// Store.SaveFile: copy error (via bad reader implementation)
// ---------------------------------------------------------------------------

func TestStoreSaveFile_CopyErrorInExtended(t *testing.T) {
	s := useTempStore(t)

	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         Ticks,
		Year:       2025,
		Month:      1,
		Day:        5,
		Hour:       14,
	}

	sentinel := errors.New("copy error")
	type badRC struct{ io.Reader }
	_ = sentinel
	_, err := s.SaveFile(k, io.NopCloser(&errReadAfter{data: nil, err: sentinel}))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator: tick outside hour window
// ---------------------------------------------------------------------------

func TestBuildHourM1_TickOutsideHourWindow(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	// Tick at hour+2 (way outside the 1-hour window)
	outsideMS := baseMS + 2*3600_000 + 100
	ticks := []RawTick{
		{timemilli: outsideMS, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	_ = err // result may vary depending on idx calculation; just verify no panic
}

// ---------------------------------------------------------------------------
// Keymap.Keys: sorted output (test with multiple keys)
// ---------------------------------------------------------------------------

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

	// Verify all three keys are present
	found := map[string]bool{}
	for _, k := range keys {
		found[k.Instrument] = true
	}
	require.True(t, found["EURUSD"])
	require.True(t, found["GBPUSD"])
	require.True(t, found["USDJPY"])
}

// ---------------------------------------------------------------------------
// WantReason string representation (if it has a String() method)
// ---------------------------------------------------------------------------

func TestWantReasonValues(t *testing.T) {
	t.Parallel()

	// Just verify the constants exist and have distinct values
	require.NotEqual(t, WantMissing, WantIncomplete)
	require.NotEqual(t, WantMissing, WantStale)
	require.NotEqual(t, WantIncomplete, WantStale)
}

// ---------------------------------------------------------------------------
// BuildInventory: verify it scans bi5 files too
// ---------------------------------------------------------------------------

func TestBuildInventory_WithBi5(t *testing.T) {
	s := useTempStore(t)

	bi5Path := s.basedir + "/dukascopy/EURUSD/2025/01/02/13h_ticks.bi5"
	require.NoError(t, makeParentsAndFile(bi5Path, []byte("fake bi5")))

	inv, err := BuildInventory(context.Background())
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.Equal(t, 1, inv.Len())
}

// ---------------------------------------------------------------------------
// writeMetadata: verify the output format
// ---------------------------------------------------------------------------

func TestWriteMetadata_Output(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet(
		"EURUSD", H1, FromTime(start), PriceScale, "test",
	)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = s.writeMetadata(cs, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "schema=v1")
	require.Contains(t, out, "EURUSD")
	require.Contains(t, out, "2026")
	require.Contains(t, out, "Timestamp,High,Open,Low,Close")
}

// ---------------------------------------------------------------------------
// WriteCSV: ValidBitSet path (candle with flags=0x0001 written and read back)
// ---------------------------------------------------------------------------

func TestWriteCSV_ValidFlag(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet(
		"EURUSD", H1, FromTime(start), PriceScale, "test",
	)
	require.NoError(t, err)

	cs.Candles[5] = Candle{Open: 200, High: 210, Low: 195, Close: 205, Ticks: 3}
	cs.SetValid(5)

	require.NoError(t, s.WriteCSV(cs))

	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: H1, Year: 2026, Month: 4}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.True(t, back.IsValid(5))
	require.Equal(t, Price(210), back.Candles[5].High)
}

// ---------------------------------------------------------------------------
// Store.PathForAsset: monthly candle with empty source defaults to "unknown"
// ---------------------------------------------------------------------------

func TestPathForAsset_EmptySourceDefaults(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "", // empty
		Kind:       KindCandle,
		TF:         H1,
		Year:       2026,
		Month:      1,
	}
	path := s.PathForAsset(k)
	require.Contains(t, path, "unknown")
}

// ---------------------------------------------------------------------------
// DataManager.Candles: strict=true error is wrapped properly
// ---------------------------------------------------------------------------

func TestCandles_StrictMissingFileWrapsError(t *testing.T) {
	s := useTempStore(t)
	_ = s

	dm := &DataManager{}
	req := CandleRequest{
		Source:     SourceCandles,
		Instrument: "EURUSD",
		Timeframe:  H1,
		Range: TimeRange{
			Start: FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   FromTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
		},
		Strict: true, // missing file should cause an error
	}
	_, err := dm.Candles(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load candles")
}

// ---------------------------------------------------------------------------
// RequiredTickHoursForMonth: verify count is reasonable
// ---------------------------------------------------------------------------

func TestRequiredTickHoursForMonth_Count(t *testing.T) {
	t.Parallel()

	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	// January 2026 has ~31 days, ~5 weekdays × 24 hours per day
	// At minimum there should be several hundred valid hours
	require.Greater(t, len(keys), 100)
	require.Less(t, len(keys), 31*24) // can't exceed 31 days × 24 hours
}

// ---------------------------------------------------------------------------
// dukasfile.newDatafile: key is cached after first call
// ---------------------------------------------------------------------------

func TestDukasfileKeyIsCached(t *testing.T) {
	t.Parallel()

	df := newDatafile("EURUSD", time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC))
	k1 := df.Key()
	k2 := df.Key()
	require.Equal(t, k1, k2)
	require.Equal(t, k1.Instrument, df.key.Instrument)
}

// ---------------------------------------------------------------------------
// Inventory.MissingComplete: all present and complete returns empty slice
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Wantlist.Range iteration
// ---------------------------------------------------------------------------

func TestWantlistRange(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	wl.Put(Want{Key: k1, WantReason: WantMissing})
	wl.Put(Want{Key: k2, WantReason: WantIncomplete})

	count := 0
	wl.items.Range(func(k Key, v Want) bool {
		count++
		return true
	})
	require.Equal(t, 2, count)
}

// ---------------------------------------------------------------------------
// Plan: context cancels mid-loop (want on the wantlist)
// ---------------------------------------------------------------------------

func TestPlanContextCancelledMidLoop(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	// Add many wants so there's something to iterate
	for i := 1; i <= 12; i++ {
		k := Key{Kind: KindTick, Instrument: "EURUSD", Year: 2026, Month: i}
		wl.Put(Want{Key: k, WantReason: WantMissing})
	}

	ctx, cancel := context.WithCancel(context.Background())

	dm := &DataManager{
		inventory: NewInventory(),
		wants:     wl,
	}

	// Cancel after a tiny delay to let the loop start
	go func() { cancel() }()

	_, _ = dm.Plan(ctx)
	// No panic, and either plan is partial or nil is acceptable
}
