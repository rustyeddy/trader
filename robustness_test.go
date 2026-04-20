// Package data - edge case and boundary tests for robustness and reliability.
// These tests verify behavior at boundaries and with unusual inputs, even when
// they don't directly add new code-coverage paths.
package trader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// Key.compare – all remaining branches + TF ordering
// =============================================================================

func TestKeyCompare_AllBranches(t *testing.T) {
	t.Parallel()

	base := Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}

	cases := []struct {
		name  string
		other Key
		want  int
	}{
		{"equal", base, 0},
		{"source <", Key{Source: "aaa", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}, 1},
		{"source >", Key{Source: "zzz", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}, -1},
		{"instrument <", Key{Source: "src", Instrument: "AUDUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}, 1},
		{"instrument >", Key{Source: "src", Instrument: "USDJPY", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}, -1},
		{"kind tick <", Key{Source: "src", Instrument: "EURUSD", Kind: KindTick, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 12}, 1},
		{"kind candle=candle", base, 0},
		{"TF M1 <", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 6, Day: 15, Hour: 12}, 1},
		{"TF D1 >", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: D1, Year: 2026, Month: 6, Day: 15, Hour: 12}, -1},
		{"year <", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2025, Month: 6, Day: 15, Hour: 12}, 1},
		{"year >", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2027, Month: 6, Day: 15, Hour: 12}, -1},
		{"month <", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1, Day: 15, Hour: 12}, 1},
		{"month >", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 12, Day: 15, Hour: 12}, -1},
		{"day <", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 1, Hour: 12}, 1},
		{"day >", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 30, Hour: 12}, -1},
		{"hour <", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 1}, 1},
		{"hour >", Key{Source: "src", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 6, Day: 15, Hour: 23}, -1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, base.compare(tc.other))
		})
	}
}

// =============================================================================
// Key.Time – boundary / edge-case values
// =============================================================================

func TestKeyTime_Boundaries(t *testing.T) {
	t.Parallel()

	t.Run("epoch year 1970", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 1970, Month: 1, Day: 1, Hour: 0}
		require.Equal(t, time.Unix(0, 0).UTC(), k.Time())
	})
	t.Run("max valid hour 23", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2026, Month: 6, Day: 15, Hour: 23}
		require.Equal(t, 23, k.Time().Hour())
	})
	t.Run("hour exactly 24 normalises to 0", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2026, Month: 6, Day: 15, Hour: 24}
		require.Equal(t, 0, k.Time().Hour())
	})
	t.Run("month exactly 12", func(t *testing.T) {
		t.Parallel()
		k := Key{Year: 2026, Month: 12, Day: 1, Hour: 0}
		require.Equal(t, time.December, k.Time().Month())
	})
	t.Run("day exactly 31 overflows to next month", func(t *testing.T) {
		t.Parallel()
		// Go's time.Date rolls over — Feb 31 2026 = Mar 3 2026 (Feb has 28 days)
		k := Key{Year: 2026, Month: 2, Day: 31, Hour: 0}
		got := k.Time()
		require.Equal(t, time.March, got.Month())
		require.Equal(t, 3, got.Day())
	})
	t.Run("negative year treated as 1970", func(t *testing.T) {
		t.Parallel()
		// Key.Time() clamps Year<=0 to 1970 (Unix epoch origin) as the default.
		k := Key{Year: -1, Month: 1, Day: 1, Hour: 0}
		require.Equal(t, 1970, k.Time().Year())
	})
}

// =============================================================================
// Key.Range – all switch branches
// =============================================================================

func TestKeyRange_AllBranches(t *testing.T) {
	t.Parallel()

	t.Run("tick hour 0 spans one hour", func(t *testing.T) {
		t.Parallel()
		k := Key{Kind: KindTick, Year: 2026, Month: 3, Day: 16, Hour: 0}
		rng := k.Range()
		require.Equal(t, Ticks, rng.TF)
		require.Equal(t, rng.End-rng.Start, Timestamp(3600))
	})
	t.Run("tick hour 23 spans exactly one hour", func(t *testing.T) {
		t.Parallel()
		k := Key{Kind: KindTick, Year: 2026, Month: 3, Day: 16, Hour: 23}
		rng := k.Range()
		require.Equal(t, Timestamp(3600), rng.End-rng.Start)
	})
	t.Run("monthly candle year boundary Dec→Jan", func(t *testing.T) {
		t.Parallel()
		k := Key{Kind: KindCandle, TF: D1, Year: 2026, Month: 12}
		rng := k.Range()
		end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		require.Equal(t, Timestamp(end.Unix()), rng.End)
	})
	t.Run("unsupported key returns zero TimeRange", func(t *testing.T) {
		t.Parallel()
		k := Key{Kind: KindCandle, Day: 5, Hour: 3}
		require.Equal(t, TimeRange{}, k.Range())
	})
}

// =============================================================================
// Tick – boundary values
// =============================================================================

func TestRawTick_ZeroAskBid(t *testing.T) {
	t.Parallel()
	tick := RawTick{Ask: 0, Bid: 0}
	require.Equal(t, Price(0), tick.Mid())
	require.Equal(t, Price(0), tick.Spread())
}

func TestRawTick_EqualAskBid(t *testing.T) {
	t.Parallel()
	tick := RawTick{Ask: 100, Bid: 100}
	require.Equal(t, Price(100), tick.Mid())
	require.Equal(t, Price(0), tick.Spread())
}

func TestRawTick_LargeValues(t *testing.T) {
	t.Parallel()
	tick := RawTick{Ask: Price(1_000_000), Bid: Price(999_000)}
	require.Equal(t, Price(999_500), tick.Mid())
	require.Equal(t, Price(1_000), tick.Spread())
}

func TestRawTick_MinuteAtHourBoundary(t *testing.T) {
	t.Parallel()
	// Exactly at the start of an hour
	tick := RawTick{timemilli: timemilli(3_600_000)}
	require.Equal(t, timemilli(3_600_000), tick.Minute())
}

func TestRawTick_MinuteJustBeforeNextHour(t *testing.T) {
	t.Parallel()
	// 59:59.999 into an hour
	tick := RawTick{timemilli: timemilli(3_599_999)}
	require.Equal(t, timemilli(3_540_000), tick.Minute()) // 59 minutes
}

// =============================================================================
// DataKind.String – all enumerated values
// =============================================================================

func TestDataKind_AllValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ticks", KindTick.String())
	require.Equal(t, "candles", KindCandle.String())
	require.Equal(t, "unknown", KindUnknown.String())

	// Values beyond the defined constants
	require.Equal(t, "unknown", DataKind(255).String())
	require.Equal(t, "unknown", DataKind(10).String())
}

// =============================================================================
// normalizeSource – edge cases
// =============================================================================

func TestNormalizeSource_EdgeCases(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", normalizeSource(""))
	require.Equal(t, "", normalizeSource("   "))
	require.Equal(t, "dukascopy", normalizeSource("DUKASCOPY"))
	require.Equal(t, "dukascopy", normalizeSource("  Dukascopy  "))
	require.Equal(t, "candles", normalizeSource("CANDLES"))
	require.Equal(t, "test-source", normalizeSource("TEST-SOURCE"))
}

// =============================================================================
// Keymap – concurrency and boundary tests
// =============================================================================

func TestKeymap_OverwriteExisting(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD", Year: 2026, Month: 1}

	km.Put(k, 1)
	km.Put(k, 2) // overwrite
	v, ok := km.Get(k)
	require.True(t, ok)
	require.Equal(t, 2, v)
}

func TestKeymap_DeleteNonExistent(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD"}
	// Should not panic
	require.NotPanics(t, func() { km.Delete(k) })
}

func TestKeymap_LenAfterOverwrite(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD"}
	km.Put(k, 1)
	km.Put(k, 2)
	require.Equal(t, 1, km.Len())
}

func TestKeymap_UpdatePropagatesValue(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD"}
	km.Put(k, 10)

	err := km.Update(k, func(v *int) error {
		*v *= 2
		return nil
	})
	require.NoError(t, err)

	v, _ := km.Get(k)
	require.Equal(t, 20, v)
}

func TestKeymap_RangeCanModifyNothing(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	km.Range(func(k Key, v int) bool {
		return false // stop immediately even if empty
	})
}

// =============================================================================
// Inventory – edge cases
// =============================================================================

func TestInventory_DoubleDelete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD"}
	inv.Put(Asset{Key: k})
	inv.Delete(k)
	inv.Delete(k) // second delete should not panic
	require.False(t, inv.Has(k))
}

func TestInventory_UpdatePreservesOtherFields(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	a := Asset{Key: k, Exists: true, Size: 1024, Reason: "test"}
	inv.Put(a)

	err := inv.Update(k, func(a *Asset) error {
		a.Complete = true
		return nil
	})
	require.NoError(t, err)

	got, ok := inv.Get(k)
	require.True(t, ok)
	require.True(t, got.Complete)
	require.True(t, got.Exists)             // unchanged
	require.Equal(t, int64(1024), got.Size) // unchanged
	require.Equal(t, "test", got.Reason)    // unchanged
}

func TestInventory_HasComplete_RequiresBothExistsAndComplete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD"}

	inv.Put(Asset{Key: k, Exists: true, Complete: false})
	require.False(t, inv.HasComplete(k))

	inv.Put(Asset{Key: k, Exists: false, Complete: true})
	require.False(t, inv.HasComplete(k))

	inv.Put(Asset{Key: k, Exists: true, Complete: true})
	require.True(t, inv.HasComplete(k))
}

func TestInventory_MissingComplete_WithDuplicateKeys(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	inv.Put(Asset{Key: k, Exists: false, Complete: false})

	// Pass the same key twice
	missing := inv.MissingComplete([]Key{k, k})
	require.Len(t, missing, 2) // both occurrences reported as missing
}

// =============================================================================
// Wantlist – edge cases
// =============================================================================

func TestWantlist_DoubleDelete(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD"}
	wl.Put(Want{Key: k, WantReason: WantMissing})
	wl.Delete(k)
	wl.Delete(k) // should not panic
	require.False(t, wl.Has(k))
}

func TestWantlist_OverwriteReason(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD"}
	wl.Put(Want{Key: k, WantReason: WantMissing})
	wl.Put(Want{Key: k, WantReason: WantStale})

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, WantStale, got.WantReason)
	require.Equal(t, 1, wl.Len())
}

func TestWantlist_AllReasonsRoundTrip(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	reasons := []WantReason{WantMissing, WantIncomplete, WantStale}
	for i, r := range reasons {
		k := Key{Instrument: "EURUSD", Year: 2026, Month: i + 1}
		wl.Put(Want{Key: k, WantReason: r})
	}
	require.Equal(t, 3, wl.Len())

	for i, r := range reasons {
		k := Key{Instrument: "EURUSD", Year: 2026, Month: i + 1}
		got, ok := wl.Get(k)
		require.True(t, ok)
		require.Equal(t, r, got.WantReason)
	}
}

// =============================================================================
// WorkState – edge cases
// =============================================================================

func TestWorkState_MarkClearIdempotent(t *testing.T) {
	t.Parallel()

	ws := NewWorkState()
	k := Key{Instrument: "EURUSD"}

	ws.MarkDownload(k)
	ws.MarkDownload(k) // idempotent
	require.True(t, ws.IsDownloadQueuedOrActive(k))

	ws.ClearDownload(k)
	ws.ClearDownload(k) // idempotent
	require.False(t, ws.IsDownloadQueuedOrActive(k))
}

func TestWorkState_DownloadAndBuildAreIndependent(t *testing.T) {
	t.Parallel()

	ws := NewWorkState()
	k := Key{Instrument: "EURUSD"}

	ws.MarkDownload(k)
	require.True(t, ws.IsDownloadQueuedOrActive(k))
	require.False(t, ws.IsBuildQueuedOrActive(k))

	ws.MarkBuild(k)
	require.True(t, ws.IsBuildQueuedOrActive(k))

	ws.ClearDownload(k)
	require.False(t, ws.IsDownloadQueuedOrActive(k))
	require.True(t, ws.IsBuildQueuedOrActive(k)) // still set
}

// =============================================================================
// Plan – boundary values
// =============================================================================

func TestPlan_Log_WithAllSections(t *testing.T) {
	t.Parallel()

	p := Plan{
		Download: make([]Key, 100),
		BuildM1:  make([]BuildTask, 50),
		BuildH1:  make([]BuildTask, 25),
		BuildD1:  make([]BuildTask, 10),
	}
	require.NotPanics(t, p.Log)
}

func TestPlan_Log_AllEmpty(t *testing.T) {
	t.Parallel()

	p := Plan{}
	require.NotPanics(t, p.Log)
}

// =============================================================================
// readNextBI5Tick – full range of valid msOffset values
// =============================================================================

func TestReadNextBI5Tick_MinOffset(t *testing.T) {
	t.Parallel()

	rec := makeBi5Record(0, 100, 99, 1.0, 0.5)
	tick, ok, err := readNextBI5Tick(bytes.NewReader(rec), "test", timemilli(1_000_000))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, timemilli(1_000_000), tick.timemilli)
}

func TestReadNextBI5Tick_MaxValidOffset(t *testing.T) {
	t.Parallel()

	// 3599999 = max valid (just under 3600*1000)
	rec := makeBi5Record(3_599_999, 200, 199, 2.0, 1.5)
	tick, ok, err := readNextBI5Tick(bytes.NewReader(rec), "test", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, timemilli(3_599_999), tick.timemilli)
}

func TestReadNextBI5Tick_ExactlyAtLimit(t *testing.T) {
	t.Parallel()

	// 3600*1000 is invalid (>= limit)
	rec := makeBi5Record(3_600_000, 100, 99, 1.0, 0.5)
	_, ok, err := readNextBI5Tick(bytes.NewReader(rec), "test", 0)
	require.Error(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_GeneralReadError_Coverage(t *testing.T) {
	t.Parallel()

	// Build exactly 10 bytes (partial record, not EOF)
	partial := make([]byte, 10)
	r := &errReadAfter{data: partial, err: errors.New("disk fail")}
	_, ok, err := readNextBI5Tick(r, "disk.bi5", 0)
	// io.ReadFull will return io.ErrUnexpectedEOF for partial data, not the wrapped error
	// because we only have 10 bytes of a 20-byte record.
	require.Error(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_ZeroVolumes(t *testing.T) {
	t.Parallel()

	rec := makeBi5Record(1000, 150, 149, 0, 0)
	tick, ok, err := readNextBI5Tick(bytes.NewReader(rec), "test", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.InDelta(t, float64(0), float64(tick.AskVol), 0.0001)
	require.InDelta(t, float64(0), float64(tick.BidVol), 0.0001)
}

func TestReadNextBI5Tick_NaNVolumes(t *testing.T) {
	t.Parallel()

	// Float32 NaN
	nanBits := math.Float32bits(float32(math.NaN()))
	rec := makeBi5Record(1000, 100, 99, math.Float32frombits(nanBits), math.Float32frombits(nanBits))
	tick, ok, err := readNextBI5Tick(bytes.NewReader(rec), "test", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, math.IsNaN(float64(tick.AskVol)))
}

// =============================================================================
// ReadCSV – boundary / edge-case rows
// =============================================================================

func TestReadCSV_EmptyFile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte{}, 0o644))

	cs, err := s.ReadCSV(k)
	require.NoError(t, err)
	require.NotNil(t, cs)
	// No candles valid
	for i := range cs.Candles {
		require.False(t, cs.IsValid(i))
	}
}

func TestReadCSV_OnlyCommentAndHeader(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: H1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(
		"# schema=v1 source=test instrument=EURUSD tf=H1 year=2026 scale=100000\n"+
			"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n",
	), 0o644))

	cs, err := s.ReadCSV(k)
	require.NoError(t, err)
	require.NotNil(t, cs)
}

func TestReadCSV_TooFewFields(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98\n", // only 4 fields
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected 9 fields")
}

func TestReadCSV_MisalignedTimestamp(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	// Timestamp is 30 seconds into a minute — not aligned to M1 (60-sec) step
	ts := time.Date(2026, time.May, 1, 0, 0, 30, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not aligned")
}

func TestReadCSV_NegativeTimestampOffset(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	// Timestamp before the month start
	ts := time.Date(2025, time.December, 31, 23, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not aligned")
}

func TestReadCSV_FlagsZero(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,0x0000\n", // flags=0, not valid
		ts.Unix(),
	)), 0o644))

	cs, err := s.ReadCSV(k)
	require.NoError(t, err)
	require.False(t, cs.IsValid(0))
}

// =============================================================================
// parseCandlePath – complete matrix of timeframe suffixes
// =============================================================================

func TestParseCandlePath_TimeframeSuffixes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		suffix string
		tf     Timeframe
		ok     bool
	}{
		{"m1", M1, true},
		{"h1", H1, true},
		{"d1", D1, true},
		{"w1", 0, false},
		// parseCandlePath lowercases the filename, so these are case-insensitive
		{"M1", M1, true},
		{"H1", H1, true},
		{"", 0, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.suffix, func(t *testing.T) {
			t.Parallel()
			var path string
			if tc.suffix == "" {
				path = "/data/candles/test/EURUSD/2026/01/EURUSD-2026-01-.csv"
			} else {
				path = fmt.Sprintf("/data/candles/test/EURUSD/2026/01/EURUSD-2026-01-%s.csv", tc.suffix)
			}
			k, ok := parseCandlePath(path)
			require.Equal(t, tc.ok, ok)
			if tc.ok {
				require.Equal(t, tc.tf, k.TF)
			}
		})
	}
}

// =============================================================================
// parseTickPath – complete boundary tests
// =============================================================================

func TestParseTickPath_BoundaryHours(t *testing.T) {
	t.Parallel()

	for _, hour := range []int{0, 1, 12, 22, 23} {
		hour := hour
		t.Run(fmt.Sprintf("hour=%d", hour), func(t *testing.T) {
			t.Parallel()
			path := fmt.Sprintf("/data/dukascopy/EURUSD/2025/06/15/%02dh_ticks.bi5", hour)
			k, ok := parseTickPath(path)
			require.True(t, ok)
			require.Equal(t, hour, k.Hour)
		})
	}
}

func TestParseTickPath_AllMonths(t *testing.T) {
	t.Parallel()

	for m := 1; m <= 12; m++ {
		m := m
		t.Run(fmt.Sprintf("month=%02d", m), func(t *testing.T) {
			t.Parallel()
			path := fmt.Sprintf("/data/dukascopy/EURUSD/2025/%02d/15/10h_ticks.bi5", m)
			k, ok := parseTickPath(path)
			require.True(t, ok)
			require.Equal(t, m, k.Month)
		})
	}
}

// =============================================================================
// candleSetIterator – range filtering
// =============================================================================

func TestCandleSetIterator_WithRange(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, H1)

	// Set valid candles at index 0 and index 5 (hours 0 and 5)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cs.Candles[0] = Candle{Open: 100, Close: 100, Ticks: 1}
	cs.SetValid(0)
	cs.Candles[5] = Candle{Open: 200, Close: 200, Ticks: 1}
	cs.SetValid(5)

	_ = s

	// Range covering only hours 0–4 (should see only index 0)
	hour3 := FromTime(start.Add(3 * time.Hour))
	rng := TimeRange{
		Start: FromTime(start),
		End:   hour3,
		TF:    H1,
	}

	it := newCandleSetIterator(cs, rng)
	count := 0
	for it.Next() {
		count++
	}
	require.NoError(t, it.Err())
	require.Equal(t, 1, count) // only the candle at index 0 is in range
}

func TestCandleSetIterator_Candle_Timestamp_AfterNext(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	_ = s

	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, H1)
	cs.Candles[2] = Candle{Open: 150, High: 160, Low: 140, Close: 155, Ticks: 5}
	cs.SetValid(2)

	it := newCandleSetIterator(cs, TimeRange{})
	require.True(t, it.Next())
	require.Equal(t, int32(5), it.Candle().Ticks)
	require.Greater(t, int64(it.Timestamp()), int64(0))
}

// =============================================================================
// chainedCandleIterator – multi-sub-iterator sequencing
// =============================================================================

func TestChainedCandleIterator_ThreeSubIterators(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	_ = s

	makeCS := func(val int) *candleSet {
		cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, H1)
		cs.Candles[0] = Candle{Open: Price(val), Ticks: 1}
		cs.SetValid(0)
		return cs
	}

	it1 := newCandleSetIterator(makeCS(100), TimeRange{})
	it2 := newCandleSetIterator(makeCS(200), TimeRange{})
	it3 := newCandleSetIterator(makeCS(300), TimeRange{})

	chained := newChainedCandleIterator(it1, it2, it3)
	count := 0
	for chained.Next() {
		count++
	}
	require.NoError(t, chained.Err())
	require.Equal(t, 3, count)
}

func TestChainedCandleIterator_ErrThenNil(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sub error")
	sub := &errCandleIterator{nextErr: sentinel}
	chained := newChainedCandleIterator(sub)

	require.False(t, chained.Next())
	require.ErrorIs(t, chained.Err(), sentinel)
	require.False(t, chained.Next()) // still false after error
}

func TestChainedCandleIterator_CloseAfterErr(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sub err")
	sub := &errCandleIterator{nextErr: sentinel}
	chained := newChainedCandleIterator(sub)

	chained.Next() // trigger error
	err := chained.Close()
	// Close may return an error if the sub-iterator close also fails; either way no panic
	_ = err
}

func TestChainedCandleIterator_AllNil(t *testing.T) {
	t.Parallel()

	chained := newChainedCandleIterator(nil, nil, nil)
	require.False(t, chained.Next())
	require.NoError(t, chained.Err())
	require.NoError(t, chained.Close())
}

// =============================================================================
// buildHourM1FromTickIterator – additional edge cases
// =============================================================================

func TestBuildHourM1_SingleTickEachMinute(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 2, 3, 8, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	// One tick per minute for all 60 minutes
	ticks := make([]RawTick, 60)
	for m := 0; m < 60; m++ {
		ticks[m] = RawTick{
			timemilli: baseMS + timemilli(m)*60_000 + 1,
			Ask:       Price(13000 + m),
			Bid:       Price(12999 + m),
		}
	}

	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tk := ticks[idx]
		idx++
		return tk, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 2, Day: 3, Hour: 8}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)
	require.Equal(t, 60, len(cs.Candles))
	for m := 0; m < 60; m++ {
		require.True(t, cs.IsValid(m), "minute %d should be valid", m)
	}
}

func TestBuildHourM1_MultipleTicksSameMinute(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 2, 3, 8, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	// Three ticks in minute 0, tracking OHLC
	ticks := []RawTick{
		{timemilli: baseMS + 100, Ask: 13010, Bid: 13000}, // open
		{timemilli: baseMS + 200, Ask: 13025, Bid: 13015}, // higher → high
		{timemilli: baseMS + 300, Ask: 13005, Bid: 12995}, // lower  → low
		{timemilli: baseMS + 400, Ask: 13012, Bid: 13002}, // close
	}

	idx := 0
	it := newFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tk := ticks[idx]
		idx++
		return tk, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 2, Day: 3, Hour: 8}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)
	require.True(t, cs.IsValid(0))
	require.Equal(t, int32(4), cs.Candles[0].Ticks)
	// High should be from the second tick's mid
	require.GreaterOrEqual(t, int32(cs.Candles[0].High), int32(cs.Candles[0].Open))
	// Low should be below open
	require.LessOrEqual(t, int32(cs.Candles[0].Low), int32(cs.Candles[0].Open))
}

// =============================================================================
// RequiredTickHoursForMonth – all months and leap-year February
// =============================================================================

func TestRequiredTickHoursForMonth_AllMonths(t *testing.T) {
	t.Parallel()

	for m := 1; m <= 12; m++ {
		m := m
		t.Run(time.Month(m).String(), func(t *testing.T) {
			t.Parallel()
			keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, m)
			require.NotEmpty(t, keys)
			for _, k := range keys {
				require.Equal(t, m, k.Month)
				require.Equal(t, 2026, k.Year)
				require.GreaterOrEqual(t, k.Day, 1)
				require.LessOrEqual(t, k.Day, 31)
			}
		})
	}
}

func TestRequiredTickHoursForMonth_LeapFeb(t *testing.T) {
	t.Parallel()

	// 2024 is a leap year — Feb has 29 days
	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2024, 2)
	require.NotEmpty(t, keys)

	maxDay := 0
	for _, k := range keys {
		if k.Day > maxDay {
			maxDay = k.Day
		}
	}
	require.LessOrEqual(t, maxDay, 29)
}

func TestRequiredTickHoursForMonth_NonLeapFeb(t *testing.T) {
	t.Parallel()

	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 2)
	require.NotEmpty(t, keys)

	maxDay := 0
	for _, k := range keys {
		if k.Day > maxDay {
			maxDay = k.Day
		}
	}
	require.LessOrEqual(t, maxDay, 28)
}

func TestRequiredTickHoursForMonth_InstrumentNormalized(t *testing.T) {
	t.Parallel()

	// EUR_USD should be normalized to EURUSD
	keys := RequiredTickHoursForMonth("dukascopy", "EUR_USD", 2026, 1)
	for _, k := range keys {
		require.Equal(t, "EURUSD", k.Instrument)
		require.NotContains(t, k.Instrument, "_")
	}
}

// =============================================================================
// FuncIterator – additional edge cases
// =============================================================================

func TestFuncIterator_NilCloseFn(t *testing.T) {
	t.Parallel()

	it := newFuncIterator(func() (int, bool, error) { return 0, false, nil }, nil)
	require.NoError(t, it.Close())
	require.NoError(t, it.Close()) // idempotent even with nil closeFn
}

func TestFuncIterator_ItemAfterClose(t *testing.T) {
	t.Parallel()

	it := newFuncIterator(func() (int, bool, error) { return 42, true, nil }, nil)
	require.True(t, it.Next())
	require.Equal(t, 42, it.Item())

	require.NoError(t, it.Close())
	require.False(t, it.Next())
	require.Equal(t, 0, it.Item()) // zero value after close
}

func TestFuncIterator_ErrThenItem(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	it := newFuncIterator(func() (int, bool, error) { return 0, false, sentinel }, nil)
	require.False(t, it.Next())
	require.Equal(t, 0, it.Item()) // zero value on error
	require.ErrorIs(t, it.Err(), sentinel)
}

// =============================================================================
// Store.Exists – edge cases with permission-denied style errors
// =============================================================================

func TestStoreExists_IsDirectory(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 7}
	path := s.PathForAsset(k)

	// Create the path as a directory instead of a file
	require.NoError(t, os.MkdirAll(path, 0o755))

	// Exists should return true (os.Stat succeeds for directories too)
	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
}

// =============================================================================
// dukasfile – boundary dates
// =============================================================================

func TestDukasfileURLAllMonths(t *testing.T) {
	t.Parallel()

	// Dukascopy uses 0-based months in URLs: Jan=00, Dec=11
	for m := 1; m <= 12; m++ {
		m := m
		t.Run(time.Month(m).String(), func(t *testing.T) {
			t.Parallel()
			df := newDatafile("EURUSD", time.Date(2025, time.Month(m), 1, 0, 0, 0, 0, time.UTC))
			url := df.URL()
			require.Contains(t, url, fmt.Sprintf("/%02d/", m-1))
		})
	}
}

func TestDukasfileTimeIsTruncatedToHour(t *testing.T) {
	t.Parallel()

	// Any sub-hour component should be dropped
	ts := time.Date(2025, 6, 15, 14, 45, 30, 999, time.UTC)
	df := newDatafile("EURUSD", ts)
	require.Equal(t, 14, df.Time.Hour())
	require.Equal(t, 0, df.Time.Minute())
	require.Equal(t, 0, df.Time.Second())
}

// =============================================================================
// bitIsSet / bitSet – boundary indices
// =============================================================================

func TestBitIsSet_Boundaries(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 4) // 256 bits total

	// Bit 0 in word 0
	bitSet(bits, 0)
	require.True(t, bitIsSet(bits, 0))
	require.False(t, bitIsSet(bits, 1))

	// Bit 63 (last bit in word 0)
	bitSet(bits, 63)
	require.True(t, bitIsSet(bits, 63))
	require.False(t, bitIsSet(bits, 62))

	// Bit 64 (first bit in word 1)
	bitSet(bits, 64)
	require.True(t, bitIsSet(bits, 64))
	require.False(t, bitIsSet(bits, 65))

	// Bit 255 (last bit in word 3)
	bitSet(bits, 255)
	require.True(t, bitIsSet(bits, 255))
	require.False(t, bitIsSet(bits, 254))
}

// =============================================================================
// newTestStore helper – used in multiple test files
// Verify helpers work correctly on their own.
// =============================================================================

func TestNewTestStore_IsIsolated(t *testing.T) {
	t.Parallel()

	s1 := newTestStore(t)
	s2 := newTestStore(t)
	require.NotEqual(t, s1.basedir, s2.basedir)
}

func TestUseTempStore_SetsGlobal(t *testing.T) {
	before := store.basedir

	s := useTempStore(t)
	require.NotEqual(t, before, store.basedir)
	require.Equal(t, s.basedir, store.basedir)
	// After test cleanup the global store is restored (handled by t.Cleanup in useTempStore)
}

// =============================================================================
// WriteCSV – validation paths
// =============================================================================

func TestWriteCSV_NilCandleSet(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	err := s.WriteCSV(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestWriteCSV_EmptyInstrument(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cs := &candleSet{Instrument: ""}
	err := s.WriteCSV(cs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "instrument")
}

func TestWriteCSV_ZeroTimeframe(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cs := &candleSet{Instrument: "EURUSD", Timeframe: 0}
	err := s.WriteCSV(cs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeframe")
}

// =============================================================================
// ReadCSV – validation paths
// =============================================================================

func TestReadCSV_NonCandleKind(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindTick, TF: Ticks, Year: 2026, Month: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "candle")
}

func TestReadCSV_InvalidMonth(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	for _, m := range []int{0, 13, -1} {
		m := m
		t.Run(fmt.Sprintf("month=%d", m), func(t *testing.T) {
			t.Parallel()
			k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: m}
			_, err := s.ReadCSV(k)
			require.Error(t, err)
			require.Contains(t, err.Error(), "month")
		})
	}
}

// =============================================================================
// Store.Delete – file-not-found is an error
// =============================================================================

func TestStoreDelete_FileNotFound(t *testing.T) {
	s := useTempStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "test",
		Kind:       KindCandle,
		TF:         M1,
		Year:       2026,
		Month:      9,
	}
	err := s.Delete(k)
	require.Error(t, err) // os.Remove on missing file returns an error
}

// =============================================================================
// Store.IsUsableTickFile – directory vs. file
// =============================================================================

func TestStoreIsUsableTickFile_Directory(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "dukascopy", Kind: KindTick, TF: Ticks, Year: 2025, Month: 3, Day: 1, Hour: 5}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(path, 0o755)) // create as dir, not file
	require.False(t, s.IsUsableTickFile(k))      // directories are not usable
}

// =============================================================================
// looksLikeHeader – variations
// =============================================================================

func TestLooksLikeHeader_Variations(t *testing.T) {
	t.Parallel()

	require.True(t, looksLikeHeader([]string{"Timestamp"}))
	require.True(t, looksLikeHeader([]string{"TIMESTAMP"}))
	require.True(t, looksLikeHeader([]string{"TIME"}))
	require.True(t, looksLikeHeader([]string{"  timestamp  "}))
	require.False(t, looksLikeHeader([]string{"1609459200"}))
	require.False(t, looksLikeHeader([]string{}))
	require.False(t, looksLikeHeader([]string{"Date"})) // "date" ≠ "timestamp" | "time"
}

// =============================================================================
// NewDataManager – fields are stored correctly
// =============================================================================

func TestNewDataManager_Fields(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	insts := []string{"EURUSD", "GBPUSD", "USDJPY"}

	dm := NewDataManager(insts, start, end)
	require.Equal(t, start, dm.Start)
	require.Equal(t, end, dm.End)
	require.Equal(t, insts, dm.Instruments)
	require.Nil(t, dm.downloader)
}

// =============================================================================
// CandleRequest.Key – all timeframes
// =============================================================================

func TestCandleRequestKey_AllTimeframes(t *testing.T) {
	t.Parallel()

	for _, tf := range []Timeframe{M1, H1, D1} {
		tf := tf
		t.Run(tf.String(), func(t *testing.T) {
			t.Parallel()
			cr := CandleRequest{Instrument: "EURUSD", Source: SourceCandles, Timeframe: tf}
			k := cr.Key()
			require.Equal(t, tf, k.TF)
			require.Equal(t, KindCandle, k.Kind)
			require.Equal(t, SourceCandles, k.Source)
		})
	}
}

// =============================================================================
// Store.PathForAsset – candle key with all timeframes
// =============================================================================

func TestPathForAsset_CandleAllTimeframes(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	for _, tf := range []struct {
		tf     Timeframe
		suffix string
	}{
		{M1, "m1"},
		{H1, "h1"},
		{D1, "d1"},
	} {
		tf := tf
		t.Run(tf.suffix, func(t *testing.T) {
			t.Parallel()
			k := Key{
				Instrument: "EURUSD",
				Source:     "test",
				Kind:       KindCandle,
				TF:         tf.tf,
				Year:       2026,
				Month:      3,
			}
			path := s.PathForAsset(k)
			require.True(t, strings.HasSuffix(path, "-"+tf.suffix+".csv"),
				"expected path to end with -%s.csv, got %s", tf.suffix, path)
		})
	}
}

// =============================================================================
// BuildWantList – boundary: empty instruments list
// =============================================================================

func TestBuildWantList_NoInstruments(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	dm := NewDataManager([]string{}, start, end)
	dm.inventory = NewInventory()

	wl, err := dm.BuildWantList(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, wl.Len())
}

// =============================================================================
// InventoryTicksComplete – partially complete (some hours missing)
// =============================================================================

func TestInventoryTicksComplete_Partial(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	// Add only the first few hours of January 2026 (all will be non-market-closed)
	// This ensures TicksComplete returns false since most hours are missing.
	base := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}

	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         Ticks,
		Year:       2026,
		Month:      1,
		Day:        3, // Monday
		Hour:       0,
	}
	inv.Put(Asset{Key: k, Exists: true, Complete: true, Size: 100})

	complete, _ := inv.TicksComplete(base)
	require.False(t, complete) // not all hours present
}
