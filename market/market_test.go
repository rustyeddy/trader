package market

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────

// monthStart returns a types.Timestamp for the first second of the given UTC month.
func monthStart(year int, month time.Month) types.Timestamp {
	t := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return types.Timestamp(t.Unix())
}

// makeM1CandleSet creates a minimal M1 CandleSet with n candles starting at ts.
func makeM1CandleSet(ts types.Timestamp, n int) *CandleSet {
	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      ts,
		Timeframe:  60,
		Scale:      1_000_000,
		Source:     "test",
		Candles:    make([]Candle, n),
		Valid:      make([]uint64, (n+63)/64),
	}
	return cs
}

// ──────────────────────────────────────────────
// Candle.IsZero
// ──────────────────────────────────────────────

func TestCandle_IsZero(t *testing.T) {
	t.Parallel()

	var zero Candle
	assert.True(t, zero.IsZero())

	nonZero := Candle{Open: 1}
	assert.False(t, nonZero.IsZero())

	tickOnly := Candle{Ticks: 1}
	assert.False(t, tickOnly.IsZero())
}

// ──────────────────────────────────────────────
// NewMonthlyCandleSet
// ──────────────────────────────────────────────

func TestNewMonthlyCandleSet_Valid(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", cs.Instrument)
	assert.Equal(t, ts, cs.Start)
	assert.Equal(t, types.Timeframe(60), cs.Timeframe)
	// January has 31 days = 44640 minutes
	assert.Equal(t, 44640, len(cs.Candles))
}

func TestNewMonthlyCandleSet_BlankInstrument(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	_, err := NewMonthlyCandleSet("", 60, ts, 1_000_000, "test")
	assert.Error(t, err)
}

func TestNewMonthlyCandleSet_BadTimeframe(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	_, err := NewMonthlyCandleSet("EURUSD", 0, ts, 1_000_000, "test")
	assert.Error(t, err)
}

func TestNewMonthlyCandleSet_NotMonthStart(t *testing.T) {
	t.Parallel()

	// mid-month timestamp
	ts := types.Timestamp(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC).Unix())
	_, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	assert.Error(t, err)
}

func TestNewMonthlyCandleSet_NotMinuteAligned(t *testing.T) {
	t.Parallel()

	// First of month but with seconds
	ts := types.Timestamp(time.Date(2025, 1, 1, 0, 0, 30, 0, time.UTC).Unix())
	_, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// AddCandle / SetValid / IsValid / CountValid
// ──────────────────────────────────────────────

func TestAddCandle_Basic(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	c := Candle{Open: 100, High: 110, Low: 90, Close: 105}
	err = cs.AddCandle(ts, c)
	require.NoError(t, err)

	assert.True(t, cs.IsValid(0))
	assert.Equal(t, c, cs.Candles[0])
	assert.Equal(t, 1, cs.CountValid())
}

func TestAddCandle_NilCandleSet(t *testing.T) {
	t.Parallel()

	var cs *CandleSet
	err := cs.AddCandle(0, Candle{})
	assert.Error(t, err)
}

func TestAddCandle_BadTimeframe(t *testing.T) {
	t.Parallel()

	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      1000,
		Timeframe:  0,
		Candles:    make([]Candle, 10),
		Valid:      make([]uint64, 1),
	}
	err := cs.AddCandle(1000, Candle{})
	assert.Error(t, err)
}

func TestAddCandle_BeforeStart(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	err = cs.AddCandle(ts-60, Candle{})
	assert.Error(t, err)
}

func TestAddCandle_NotAligned(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	// 30 seconds past the start is not minute-aligned
	err = cs.AddCandle(ts+30, Candle{})
	assert.Error(t, err)
}

func TestAddCandle_OutOfRange(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	// Way past end of January
	future := types.Timestamp(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC).Unix())
	err = cs.AddCandle(future, Candle{})
	assert.Error(t, err)
}

func TestAddCandle_Duplicate(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	c1 := Candle{Open: 100, High: 110, Low: 90, Close: 105}
	c2 := Candle{Open: 200, High: 210, Low: 190, Close: 205}

	require.NoError(t, cs.AddCandle(ts, c1))
	require.NoError(t, cs.AddCandle(ts, c2)) // duplicate — should overwrite without error
	assert.Equal(t, c2, cs.Candles[0])
}

// ──────────────────────────────────────────────
// SetValid / IsValid helpers
// ──────────────────────────────────────────────

func TestSetValidIsValid(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(1000*60, 200)
	assert.False(t, cs.IsValid(63))
	cs.SetValid(63)
	assert.True(t, cs.IsValid(63))
	assert.False(t, cs.IsValid(64))
}

// ──────────────────────────────────────────────
// Merge
// ──────────────────────────────────────────────

func TestMerge_Basic(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	src, err := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	require.NoError(t, err)

	c := Candle{Open: 100, High: 110, Low: 90, Close: 105}
	require.NoError(t, src.AddCandle(ts, c))

	err = dst.Merge(src)
	require.NoError(t, err)
	assert.Equal(t, 1, dst.CountValid())
	assert.Equal(t, c, dst.Candles[0])
}

func TestMerge_NilError(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")

	assert.Error(t, dst.Merge(nil))

	var nilDst *CandleSet
	assert.Error(t, nilDst.Merge(dst))
}

func TestMerge_TimeframeMismatch(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	src, _ := NewMonthlyCandleSet("EURUSD", 3600, ts, 1_000_000, "test")
	assert.Error(t, dst.Merge(src))
}

func TestMerge_ScaleMismatch(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	src, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 100_000, "test")
	assert.Error(t, dst.Merge(src))
}

func TestMerge_InstrumentMismatch(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")
	src, _ := NewMonthlyCandleSet("GBPUSD", 60, ts, 1_000_000, "test")
	assert.Error(t, dst.Merge(src))
}

func TestMerge_BlankInstrument(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	dst, _ := NewMonthlyCandleSet("EURUSD", 60, ts, 1_000_000, "test")

	src := &CandleSet{
		Instrument: "",
		Start:      ts,
		Timeframe:  60,
		Scale:      1_000_000,
		Candles:    make([]Candle, 10),
		Valid:      make([]uint64, 1),
	}
	assert.Error(t, dst.Merge(src))
}

// ──────────────────────────────────────────────
// FloorToMonthUTC
// ──────────────────────────────────────────────

func TestFloorToMonthUTC(t *testing.T) {
	t.Parallel()

	midJan := types.Timestamp(time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC).Unix())
	got := FloorToMonthUTC(midJan)
	expected := types.Timestamp(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	assert.Equal(t, expected, got)

	// Already at month start should be a no-op
	ts := monthStart(2025, time.February)
	assert.Equal(t, ts, FloorToMonthUTC(ts))
}

// ──────────────────────────────────────────────
// CandleSet.Time / CandleSet.Timestamp
// ──────────────────────────────────────────────

func TestCandleSet_TimeAndTimestamp(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs := makeM1CandleSet(ts, 5)

	assert.Equal(t, time.Unix(int64(ts), 0).UTC(), cs.Time(0))
	assert.Equal(t, time.Unix(int64(ts)+60, 0).UTC(), cs.Time(1))
	assert.Equal(t, ts, cs.Timestamp(0))
	assert.Equal(t, ts+60, cs.Timestamp(1))
}

// ──────────────────────────────────────────────
// CandleSet.Filename
// ──────────────────────────────────────────────

func TestCandleSet_Filename(t *testing.T) {
	t.Parallel()

	ts := monthStart(2025, time.January)
	cs := makeM1CandleSet(ts, 1)
	cs.Timeframe = 60
	assert.Equal(t, "eurusd-m1-2025", cs.Filename())

	// D1 uses a different format
	cs2 := makeM1CandleSet(ts, 1)
	cs2.Timeframe = 86400
	assert.Equal(t, "eurusd-d1-all", cs2.Filename())
}

// ──────────────────────────────────────────────
// BuildGapReport / classifyGap / Stats
// ──────────────────────────────────────────────

func makeDenseCS(start types.Timestamp, n int, validIdxs []int) *CandleSet {
	cs := makeM1CandleSet(start, n)
	for _, i := range validIdxs {
		cs.Candles[i] = Candle{Open: 100, High: 110, Low: 90, Close: 105, Ticks: 1}
		cs.SetValid(i)
	}
	return cs
}

func TestBuildGapReport_NoGaps(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix()) // Monday
	n := 10
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	cs := makeDenseCS(start, n, all)
	cs.BuildGapReport()
	assert.Len(t, cs.Gaps, 0)
}

func TestBuildGapReport_OneGap(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix()) // Monday
	// 10 candles, only 0-4 and 8-9 valid; gap at 5-7
	cs := makeDenseCS(start, 10, []int{0, 1, 2, 3, 4, 8, 9})
	cs.BuildGapReport()
	require.Len(t, cs.Gaps, 1)
	assert.Equal(t, int32(5), cs.Gaps[0].StartIdx)
	assert.Equal(t, int32(3), cs.Gaps[0].Len)
}

func TestStats_Basic(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix())
	cs := makeDenseCS(start, 10, []int{0, 1, 2, 3, 4, 8, 9})
	s := cs.Stats()
	assert.Equal(t, 10, s.TotalMinutes)
	assert.Equal(t, 7, s.PresentMinutes)
	assert.Equal(t, 3, s.MissingMinutes)
	assert.Equal(t, 1, s.GapCount)
}

func TestClassifyGap_Weekend(t *testing.T) {
	t.Parallel()

	// A Friday with a 1440-minute (24h) gap should be "weekend"
	friday := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC) // a Friday
	cs := makeM1CandleSet(types.Timestamp(friday.Unix()), 100)
	kind := cs.classifyGap(0, 1440) // 1440 minutes = 24h
	assert.Equal(t, "weekend", kind)
}

func TestClassifyGap_Suspicious(t *testing.T) {
	t.Parallel()

	// A Monday with a 1440-minute gap should be "suspicious"
	monday := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	cs := makeM1CandleSet(types.Timestamp(monday.Unix()), 100)
	kind := cs.classifyGap(0, 1440)
	assert.Equal(t, "suspicious", kind)
}

func TestClassifyGap_Minor(t *testing.T) {
	t.Parallel()

	monday := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	cs := makeM1CandleSet(types.Timestamp(monday.Unix()), 100)
	kind := cs.classifyGap(0, 5) // 5 minutes – minor
	assert.Equal(t, "minor", kind)
}

func TestClassifyGap_SuspiciousShort(t *testing.T) {
	t.Parallel()

	monday := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	cs := makeM1CandleSet(types.Timestamp(monday.Unix()), 100)
	kind := cs.classifyGap(0, 15) // 15 minutes, Monday → suspicious
	assert.Equal(t, "suspicious", kind)
}

// ──────────────────────────────────────────────
// AggregateH1
// ──────────────────────────────────────────────

func TestAggregateH1_Basic(t *testing.T) {
	t.Parallel()

	// Build a 120-minute CandleSet starting exactly on an hour boundary.
	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 120)
	// Fill 60 minutes for hour 0 and 60 minutes for hour 1
	for i := 0; i < 120; i++ {
		cs.Candles[i] = Candle{Open: 100, High: 110, Low: 90, Close: 105, Ticks: 1}
		cs.SetValid(i)
	}

	h1 := cs.AggregateH1(1)
	assert.Equal(t, types.Timeframe(3600), h1.Timeframe)
	assert.True(t, h1.IsValid(0))
	assert.True(t, h1.IsValid(1))
	assert.Equal(t, types.Price(100), h1.Candles[0].Open)
	assert.Equal(t, types.Price(110), h1.Candles[0].High)
	assert.Equal(t, types.Price(90), h1.Candles[0].Low)
	assert.Equal(t, types.Price(105), h1.Candles[0].Close)
}

func TestAggregateH1_PanicOnWrongTF(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(1000*60, 10)
	cs.Timeframe = 3600 // not M1
	assert.Panics(t, func() { cs.AggregateH1(1) })
}

func TestAggregateH1_MinValidClamp(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 60)
	for i := 0; i < 60; i++ {
		cs.Candles[i] = Candle{Open: 100, High: 110, Low: 90, Close: 105, Ticks: 1}
		cs.SetValid(i)
	}

	// minValid 0 should be clamped to 1
	h1 := cs.AggregateH1(0)
	assert.True(t, h1.IsValid(0))

	// minValid > 60 should be clamped to 60; with only 60 valid bars it still passes
	h1b := cs.AggregateH1(100)
	assert.True(t, h1b.IsValid(0))
}

// ──────────────────────────────────────────────
// Float64 / Int32
// ──────────────────────────────────────────────

func TestFloat64AndInt32(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Scale = 1_000_000
	assert.InDelta(t, 1.035030, cs.Float64(1_035_030), 1e-9)
	assert.Equal(t, int32(1_035_030), cs.Int32(1.035030))
}

// ──────────────────────────────────────────────
// PipSize / UnitsPerPip / DeltaToPips / PipsToDelta
// ──────────────────────────────────────────────

func TestPipSize_KnownInstrument(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Scale = 1_000_000
	cs.Instrument = "EURUSD"
	pip := cs.PipSize()
	assert.InDelta(t, 0.0001, pip, 1e-12) // PipLocation = -4
}

func TestPipSize_UnknownInstrument(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Instrument = "UNKNOWN"
	assert.Equal(t, 0.0, cs.PipSize())
}

func TestUnitsPerPip(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Scale = 1_000_000
	cs.Instrument = "EURUSD"
	// 1e6 * 1e-4 = 100
	assert.InDelta(t, 100.0, cs.UnitsPerPip(), 1e-9)
}

func TestDeltaToPipsAndBack(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Scale = 1_000_000
	cs.Instrument = "EURUSD"

	pips := cs.DeltaToPips(100)
	assert.InDelta(t, 1.0, pips, 1e-9)

	delta := cs.PipsToDelta(1.0)
	assert.Equal(t, int32(100), delta)
}

// ──────────────────────────────────────────────
// PrintStats (smoke test – just confirm no panic)
// ──────────────────────────────────────────────

func TestPrintStats_NoFileNoData(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix())
	cs := makeDenseCS(start, 10, []int{0, 1, 2, 3, 4, 8, 9})

	var buf bytes.Buffer
	w := &nopCloser{&buf}
	cs.PrintStats(w)
	assert.Contains(t, buf.String(), "CandleSet Stats")
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// ──────────────────────────────────────────────
// Aggregate
// ──────────────────────────────────────────────

func TestAggregate_M1toH1(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 120)
	for i := 0; i < 120; i++ {
		cs.Candles[i] = Candle{Open: 100, High: 110, Low: 90, Close: 105, Ticks: 1}
		cs.SetValid(i)
	}

	out, err := cs.Aggregate(3600)
	require.NoError(t, err)
	assert.Equal(t, types.Timeframe(3600), out.Timeframe)
	assert.Equal(t, 2, len(out.Candles))
	assert.True(t, out.IsValid(0))
	assert.True(t, out.IsValid(1))
}

func TestAggregate_Errors(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 10)

	var nilCS *CandleSet
	_, err := nilCS.Aggregate(3600)
	assert.Error(t, err)

	cs.Timeframe = 0
	_, err = cs.Aggregate(3600)
	assert.Error(t, err)

	cs.Timeframe = 60
	_, err = cs.Aggregate(0)
	assert.Error(t, err)

	// outTF not a multiple of cs.Timeframe
	_, err = cs.Aggregate(7200 + 1)
	assert.Error(t, err)
}

func TestAggregate_EmptyCandles(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 120)
	// no valid candles
	out, err := cs.Aggregate(3600)
	require.NoError(t, err)
	assert.False(t, out.IsValid(0))
}

// ──────────────────────────────────────────────
// Iterator
// ──────────────────────────────────────────────

func TestIterator_Basic(t *testing.T) {
	t.Parallel()

	ts := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(ts, 5)
	for i := 0; i < 5; i++ {
		cs.Candles[i] = Candle{Open: types.Price(i + 1), Ticks: 1}
		cs.SetValid(i)
	}

	it := cs.Iterator()
	count := 0
	for it.Next() {
		count++
		assert.Equal(t, count-1, it.Index())
		assert.Equal(t, types.Price(count), it.Candle().Open)
		assert.Equal(t, cs.Timestamp(count-1), it.Timestamp())
		assert.Equal(t, cs.Time(count-1), it.Time())
	}
	assert.Equal(t, 5, count)
	assert.Equal(t, ts, it.StartTime())
}

func TestIterator_SparseValid(t *testing.T) {
	t.Parallel()

	ts := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(ts, 10)
	// only indices 2 and 7 valid
	cs.Candles[2] = Candle{Open: 100, Ticks: 1}
	cs.SetValid(2)
	cs.Candles[7] = Candle{Open: 200, Ticks: 1}
	cs.SetValid(7)

	it := cs.Iterator()
	var indices []int
	for it.Next() {
		indices = append(indices, it.Index())
	}
	assert.Equal(t, []int{2, 7}, indices)
}

func TestIterator_Empty(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(1000*60, 5)
	it := cs.Iterator()
	assert.False(t, it.Next())
}

// ──────────────────────────────────────────────
// instruments.go: GetInstrument / NormalizeInstrument
// ──────────────────────────────────────────────

func TestGetInstrument(t *testing.T) {
	t.Parallel()

	inst := GetInstrument("EURUSD")
	require.NotNil(t, inst)
	assert.Equal(t, "EURUSD", inst.Name)

	// via Symmap (EUR_USD → EURUSD)
	inst2 := GetInstrument("EUR_USD")
	require.NotNil(t, inst2)
	assert.Equal(t, "EURUSD", inst2.Name)

	assert.Nil(t, GetInstrument("NO_SUCH"))

	// Symmap value that resolves to a non-existent key (shouldn't happen in prod, but test coverage)
	inst3 := GetInstrument("XAUUSD")
	require.NotNil(t, inst3)
	assert.Equal(t, "XAUUSD", inst3.Name)
}

func TestNormalizeInstrument(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "EURUSD", NormalizeInstrument("EUR_USD"))
	assert.Equal(t, "EURUSD", NormalizeInstrument("EUR/USD"))
	assert.Equal(t, "EURUSD", NormalizeInstrument("eurusd"))
	assert.Equal(t, "EURUSD", NormalizeInstrument("  eurusd  "))
}

// ──────────────────────────────────────────────
// tick.go: Spread
// ──────────────────────────────────────────────

func TestTick_Spread(t *testing.T) {
	t.Parallel()

	tick := Tick{BA: BA{Bid: 100, Ask: 103}}
	assert.Equal(t, types.Price(3), tick.Spread())
}

// ──────────────────────────────────────────────
// utils.go: IsFXMarketClosed
// ──────────────────────────────────────────────

func TestIsFXMarketClosed(t *testing.T) {
	t.Parallel()

	sat := time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC) // Saturday
	assert.True(t, IsFXMarketClosed(sat))

	sunClosed := time.Date(2025, 1, 5, 10, 0, 0, 0, time.UTC) // Sunday 10:00 < 22
	assert.True(t, IsFXMarketClosed(sunClosed))

	sunOpen := time.Date(2025, 1, 5, 22, 0, 0, 0, time.UTC) // Sunday 22:00 >= 22
	assert.False(t, IsFXMarketClosed(sunOpen))

	friClosed := time.Date(2025, 1, 3, 22, 0, 0, 0, time.UTC) // Friday 22:00 >= 22
	assert.True(t, IsFXMarketClosed(friClosed))

	friOpen := time.Date(2025, 1, 3, 21, 0, 0, 0, time.UTC) // Friday 21:00 < 22
	assert.False(t, IsFXMarketClosed(friOpen))

	mon := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)
	assert.False(t, IsFXMarketClosed(mon))
}

// ──────────────────────────────────────────────
// utils.go: parseToUnix
// ──────────────────────────────────────────────

func TestParseToUnix(t *testing.T) {
	t.Parallel()

	// RFC3339 format
	ts, err := parseToUnix("2025-01-06T10:00:00Z")
	require.NoError(t, err)
	expected := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	assert.Equal(t, expected, ts)

	// layout "20060102 150405"
	ts2, err := parseToUnix("20250106 100000")
	require.NoError(t, err)
	assert.NotZero(t, ts2)

	// invalid
	_, err = parseToUnix("not-a-date")
	assert.Error(t, err)

	// non-minute-aligned RFC3339
	_, err = parseToUnix("2025-01-06T10:00:30Z")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// utils.go: parseEST
// ──────────────────────────────────────────────

func TestParseEST(t *testing.T) {
	t.Parallel()

	tm, err := parseEST("20250106 100000")
	require.NoError(t, err)
	// EST = UTC-5, so 10:00 EST = 15:00 UTC
	assert.Equal(t, 15, tm.Hour())

	_, err = parseEST("not a date")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// utils.go: fastPrice
// ──────────────────────────────────────────────

func TestFastPrice(t *testing.T) {
	t.Parallel()

	p, err := fastPrice("1.035030")
	require.NoError(t, err)
	assert.Equal(t, types.Price(1035030), p)

	p2, err := fastPrice("1234567")
	require.NoError(t, err)
	assert.Equal(t, types.Price(1234567), p2)

	_, err = fastPrice("not-a-number")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// utils.go: bitIsSet / bitSet
// ──────────────────────────────────────────────

func TestBitOps(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)
	assert.False(t, bitIsSet(bits, 0))
	assert.False(t, bitIsSet(bits, 63))
	assert.False(t, bitIsSet(bits, 64))

	bitSet(bits, 0)
	assert.True(t, bitIsSet(bits, 0))
	assert.False(t, bitIsSet(bits, 1))

	bitSet(bits, 63)
	assert.True(t, bitIsSet(bits, 63))

	bitSet(bits, 64)
	assert.True(t, bitIsSet(bits, 64))
	assert.False(t, bitIsSet(bits, 65))
}

// ──────────────────────────────────────────────
// utils.go: SecondsToTFString
// ──────────────────────────────────────────────

func TestSecondsToTFString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sec  types.Timestamp
		want string
	}{
		{60, "M1"},
		{300, "M5"},
		{1800, "M30"},
		{3600, "H1"},
		{14400, "H4"},
		{86400, "D1"},
		{7 * 86400, "W1"},
		{30 * 86400, "MN1"},
	}
	for _, tc := range cases {
		s, err := SecondsToTFString(tc.sec)
		require.NoError(t, err, "sec=%d", tc.sec)
		assert.Equal(t, tc.want, s, "sec=%d", tc.sec)
	}

	_, err := SecondsToTFString(0)
	assert.Error(t, err)

	_, err = SecondsToTFString(7777)
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// utils.go: TFStringToSeconds
// ──────────────────────────────────────────────

func TestTFStringToSeconds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tf   string
		want types.Timestamp
	}{
		{"M1", 60},
		{"M5", 300},
		{"M15", 900},
		{"M30", 1800},
		{"H1", 3600},
		{"H4", 14400},
		{"D1", 86400},
		{"W1", 604800},
		{"MN1", 2592000},
	}
	for _, tc := range cases {
		sec, err := TFStringToSeconds(tc.tf)
		require.NoError(t, err, "tf=%s", tc.tf)
		assert.Equal(t, tc.want, sec, "tf=%s", tc.tf)
	}

	_, err := TFStringToSeconds("INVALID")
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// conversion.go: QuoteToAccountRate edge cases
// ──────────────────────────────────────────────

func TestQuoteToAccountRate_ZeroMid(t *testing.T) {
	t.Parallel()

	// Find an instrument where base == account; provide a tick with mid=0
	instrument, ok := findByBase("USD")
	if !ok {
		t.Skip("no instrument with base currency USD")
	}

	ps := &fakePriceSource{
		price: Tick{BA: BA{Bid: 0, Ask: 0}},
	}
	_, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.Error(t, err)
}

func TestQuoteToAccountRate_TickError(t *testing.T) {
	t.Parallel()

	instrument, ok := findByBase("USD")
	if !ok {
		t.Skip("no instrument with base currency USD")
	}

	ps := &fakePriceSource{
		err: assert.AnError,
	}
	_, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.Error(t, err)
}

// ──────────────────────────────────────────────
// seValid / isValid (package-level helpers)
// ──────────────────────────────────────────────

func TestPackageLevelSetIsValid(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)
	assert.False(t, isValid(bits, 5))
	setValid(bits, 5)
	assert.True(t, isValid(bits, 5))

	// crossing word boundary
	assert.False(t, isValid(bits, 64))
	setValid(bits, 64)
	assert.True(t, isValid(bits, 64))
}

// ──────────────────────────────────────────────
// Aggregate with no valid bits (hasValidBits = false)
// ──────────────────────────────────────────────

func TestAggregate_NoValidBits(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 120)
	cs.Valid = nil // simulate no valid-bit array

	for i := 0; i < 120; i++ {
		cs.Candles[i] = Candle{Open: 100, High: 110, Low: 90, Close: 105, Ticks: 1}
	}

	out, err := cs.Aggregate(3600)
	require.NoError(t, err)
	// All candles treated as valid when Valid slice is nil
	assert.True(t, out.IsValid(0))
}

// ──────────────────────────────────────────────
// Stats calls BuildGapReport when gaps not populated
// ──────────────────────────────────────────────

func TestStats_CallsBuildGapReport(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC).Unix())
	cs := makeDenseCS(start, 10, []int{0, 1, 2})
	// Gaps is nil; Stats should call BuildGapReport internally
	cs.Gaps = nil
	s := cs.Stats()
	assert.Equal(t, 10, s.TotalMinutes)
	assert.Equal(t, 3, s.PresentMinutes)
}

// ──────────────────────────────────────────────
// SecondsToTFString – multi-day cases
// ──────────────────────────────────────────────

func TestSecondsToTFString_MultiDay(t *testing.T) {
	t.Parallel()

	// 2 days
	s, err := SecondsToTFString(2 * 86400)
	require.NoError(t, err)
	assert.Equal(t, "D2", s)
}

// ──────────────────────────────────────────────
// PipSize for USDJPY (PipLocation=-2)
// ──────────────────────────────────────────────

func TestPipSize_USDJPY(t *testing.T) {
	t.Parallel()

	cs := makeM1CandleSet(0, 1)
	cs.Scale = 1_000_000
	cs.Instrument = "USDJPY"
	pip := cs.PipSize()
	assert.InDelta(t, 0.01, pip, 1e-12) // PipLocation = -2
}

// ──────────────────────────────────────────────
// Stats with LongestGap tracking
// ──────────────────────────────────────────────

func TestStats_LongestGap(t *testing.T) {
	t.Parallel()

	// Saturday start → weekend classification
	saturday := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)
	// Use enough candles: a big gap on Saturday (weekend)
	// gap at index 0 of length 1500 minutes (> 24h)
	start := types.Timestamp(saturday.Unix())
	n := 2000
	cs := makeM1CandleSet(start, n)
	// only the last 400 candles valid
	for i := 1600; i < n; i++ {
		cs.Candles[i] = Candle{Open: 100, Ticks: 1}
		cs.SetValid(i)
	}

	s := cs.Stats()
	assert.Equal(t, 1600, s.LongestGap)
	assert.Equal(t, "weekend", s.LongestGapKind)
}

// ──────────────────────────────────────────────
// Tick.Mid – boundary / rounding
// ──────────────────────────────────────────────

func TestTick_Mid(t *testing.T) {
	t.Parallel()

	// Even sum
	tick1 := Tick{BA: BA{Bid: 100, Ask: 102}}
	assert.Equal(t, types.Price(101), tick1.Mid())

	// Odd sum rounds up
	tick2 := Tick{BA: BA{Bid: 100, Ask: 101}}
	assert.Equal(t, types.Price(101), tick2.Mid())
}

// ──────────────────────────────────────────────
// Aggregate – tick-count and avg-spread calculation
// ──────────────────────────────────────────────

func TestAggregate_SpreadAndTicks(t *testing.T) {
	t.Parallel()

	start := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	cs := makeM1CandleSet(start, 2)
	cs.Candles[0] = Candle{Open: 100, High: 105, Low: 95, Close: 102, AvgSpread: 10, MaxSpread: 12, Ticks: 5}
	cs.SetValid(0)
	cs.Candles[1] = Candle{Open: 102, High: 108, Low: 98, Close: 106, AvgSpread: 20, MaxSpread: 25, Ticks: 10}
	cs.SetValid(1)

	out, err := cs.Aggregate(120)
	require.NoError(t, err)
	require.Equal(t, 1, len(out.Candles))

	bar := out.Candles[0]
	assert.Equal(t, types.Price(100), bar.Open)
	assert.Equal(t, types.Price(108), bar.High)
	assert.Equal(t, types.Price(95), bar.Low)
	assert.Equal(t, types.Price(106), bar.Close)
	assert.Equal(t, int32(15), bar.Ticks) // 5 + 10
	assert.Equal(t, types.Price(25), bar.MaxSpread)

	// weighted avg spread with rounding: (10*5 + 20*10 + 15/2) / 15 = 257/15 = 17
	assert.Equal(t, types.Price(17), bar.AvgSpread)
}

// ──────────────────────────────────────────────
// Candle IsZero edge cases
// ──────────────────────────────────────────────

func TestCandle_IsZero_AllFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		c    Candle
		want bool
	}{
		{"open set", Candle{Open: 1}, false},
		{"high set", Candle{High: 1}, false},
		{"low set", Candle{Low: 1}, false},
		{"close set", Candle{Close: 1}, false},
		{"ticks set", Candle{Ticks: 1}, false},
		{"zero", Candle{}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.c.IsZero())
		})
	}
}

// ──────────────────────────────────────────────
// NormalizeInstrument edge cases
// ──────────────────────────────────────────────

func TestNormalizeInstrument_EdgeCases(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "EURUSD", NormalizeInstrument("EUR/USD"))
	assert.Equal(t, "EURUSD", NormalizeInstrument("eur_usd"))
	assert.Equal(t, "", NormalizeInstrument(""))
}

// ──────────────────────────────────────────────
// math consistency: PipSize uses math.Pow10
// ──────────────────────────────────────────────

func TestPipSize_AllInstruments(t *testing.T) {
	t.Parallel()

	for name, inst := range Instruments {
		name, inst := name, inst
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := makeM1CandleSet(0, 1)
			cs.Scale = 1_000_000
			cs.Instrument = name
			pip := cs.PipSize()
			expected := math.Pow10(inst.PipLocation)
			assert.InDelta(t, expected, pip, 1e-12)
		})
	}
}

// ──────────────────────────────────────────────
// scanBounds / buildDenseFromFile  (require temp CSV files)
// ──────────────────────────────────────────────

// writeTempCSV creates a temporary semicolon-delimited CSV file that
// buildDenseFromFile / scanBounds can parse.
func writeTempCSV(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "candles-*.csv")
	require.NoError(t, err)
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	return f.Name()
}

// candleRow formats a single candle row in the semicolon-delimited format expected
// by buildDenseFromFile (RFC3339 timestamp, 6-decimal prices, ticks, valid-flag).
func candleRow(ts types.Timestamp, open, high, low, close_ float64, ticks int, valid int) string {
	t := time.Unix(int64(ts), 0).UTC()
	return fmt.Sprintf("%s;%.6f;%.6f;%.6f;%.6f;0.000000;0.000000;%d;%d",
		t.Format(time.RFC3339), open, high, low, close_, ticks, valid)
}

func TestScanBounds_Basic(t *testing.T) {
	t.Parallel()

	// Two candles one minute apart on a Monday
	ts0 := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	ts1 := ts0 + 60

	lines := []string{
		"# comment",
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		candleRow(ts0, 1.035030, 1.035140, 1.035030, 1.035140, 10, 1),
		candleRow(ts1, 1.035140, 1.035200, 1.035100, 1.035190, 8, 1),
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}

	minTs, maxTs, err := cs.scanBounds()
	require.NoError(t, err)
	assert.Equal(t, ts0, minTs)
	assert.Equal(t, ts1, maxTs)
}

func TestScanBounds_FileNotFound(t *testing.T) {
	t.Parallel()

	cs := &CandleSet{Filepath: "/no/such/file.csv"}
	_, _, err := cs.scanBounds()
	assert.Error(t, err)
}

func TestScanBounds_NoValidTimestamps(t *testing.T) {
	t.Parallel()

	lines := []string{
		"# only comments",
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		"bad-data-line",
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}
	_, _, err := cs.scanBounds()
	assert.Error(t, err)
}

func TestBuildDenseFromFile_Basic(t *testing.T) {
	t.Parallel()

	ts0 := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())
	ts1 := ts0 + 60
	ts2 := ts0 + 120

	lines := []string{
		"# header",
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		candleRow(ts0, 1.035030, 1.035140, 1.035010, 1.035140, 10, 1),
		candleRow(ts1, 1.035140, 1.035200, 1.035100, 1.035180, 8, 1),
		candleRow(ts2, 1.035180, 1.035300, 1.035080, 1.035250, 12, 1),
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}

	err := cs.buildDenseFromFile()
	require.NoError(t, err)
	assert.Equal(t, 3, len(cs.Candles))
	assert.Equal(t, 3, cs.CountValid())
	assert.Equal(t, ts0, cs.Start)
}

func TestBuildDenseFromFile_FileNotFound(t *testing.T) {
	t.Parallel()

	cs := &CandleSet{Filepath: "/no/such/file.csv"}
	err := cs.buildDenseFromFile()
	assert.Error(t, err)
}

func TestBuildDenseFromFile_DuplicateLines(t *testing.T) {
	t.Parallel()

	ts0 := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())

	lines := []string{
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		candleRow(ts0, 1.035030, 1.035140, 1.035010, 1.035140, 10, 1),
		candleRow(ts0, 1.036000, 1.036100, 1.035900, 1.036050, 5, 1), // duplicate
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}

	err := cs.buildDenseFromFile()
	require.NoError(t, err)
	// duplicate should be skipped
	assert.Equal(t, 1, cs.CountValid())
}

func TestBuildDenseFromFile_BadLines(t *testing.T) {
	t.Parallel()

	ts0 := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())

	lines := []string{
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		"too;few;cols", // bad: < 9 parts
		candleRow(ts0, 1.035030, 1.035140, 1.035010, 1.035140, 10, 1),
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}

	err := cs.buildDenseFromFile()
	require.NoError(t, err)
	assert.Equal(t, 1, cs.CountValid())
}

func TestBuildDenseFromFile_InvalidFlag(t *testing.T) {
	t.Parallel()

	ts0 := types.Timestamp(time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC).Unix())

	// valid=0 means the bit should NOT be set
	lines := []string{
		"Time;Open;High;Low;Close;AvgSpread;MaxSpread;Ticks;Valid",
		candleRow(ts0, 1.035030, 1.035140, 1.035010, 1.035140, 10, 0),
	}
	fname := writeTempCSV(t, lines)
	cs := &CandleSet{Filepath: fname}

	err := cs.buildDenseFromFile()
	require.NoError(t, err)
	assert.Equal(t, 0, cs.CountValid())
}
