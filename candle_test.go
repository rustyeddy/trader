package trader

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bufferWriteCloser is a test helper that wraps bytes.Buffer as a WriteCloser.
type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error { return nil }

// loadCandleSet loads the shared M1 test dataset, skipping if it is absent.
func loadCandleSet(t *testing.T) *candleSet {
	t.Helper()
	fname := "./testdata/DAT_ASCII_EURUSD_M1_2025.csv"
	if _, err := os.Stat(fname); err != nil {
		t.Skip("candle test dataset missing")
	}
	return &candleSet{Filepath: fname}
}

// --- file-based tests (skip when testdata is absent) ---

func TestIterator(t *testing.T) {
	cs := loadCandleSet(t)

	expected := Candle{Open: 1035030, High: 1035140, Low: 1035030, Close: 1035140}
	it := cs.Iterator()
	it.Next()
	assert.Equal(t, Timestamp(1735768800), it.StartTime())
	assert.Equal(t, expected, it.Candle())

	i := 0
	for it.Next() {
		i++
	}
	assert.Equal(t, 372023, i)
}

func TestReadCandleSetFile(t *testing.T) {
	cs := loadCandleSet(t)
	s := cs.Stats()
	assert.Equal(t, 524158, s.TotalBars)
	assert.Equal(t, 372024, s.PresentBars)
	assert.Equal(t, 152134, s.MissingBars)
	assert.Equal(t, 965, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 15, s.SuspiciousGaps)
}

func TestAggregateH1(t *testing.T) {
	cs := loadCandleSet(t)
	h1, err := cs.AggregateH1(50)
	require.NoError(t, err)
	h1.BuildGapReport()
	s := h1.Stats()
	assert.Equal(t, 8736, s.TotalBars)
	assert.Equal(t, 6212, s.PresentBars)
	assert.Equal(t, 2524, s.MissingBars)
	assert.Equal(t, 54, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 2, s.SuspiciousGaps)
	assert.Equal(t, 49, s.LongestGapBars)
}

// --- unit tests (synthetic data, always run) ---

func TestNewMonthlyCandleSet_Guards(t *testing.T) {
	t.Parallel()

	monthStart := Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())

	_, err := newMonthlyCandleSet("", M1, monthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")

	_, err = newMonthlyCandleSet("EURUSD", TF0, monthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")

	badMinute := Timestamp(time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC).Unix())
	_, err = newMonthlyCandleSet("EURUSD", M1, badMinute, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minute boundary")

	badMonthStart := Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix())
	_, err = newMonthlyCandleSet("EURUSD", M1, badMonthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start of month")
}

func TestCandleSetAddCandle_Branches(t *testing.T) {
	t.Parallel()

	c := Candle{Open: 1, High: 2, Low: 1, Close: 2, Ticks: 1}

	var nilSet *candleSet
	err := nilSet.AddCandle(1, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil CandleSet")

	cs := &candleSet{Start: 100, Timeframe: M1, Candles: make([]Candle, 2), Valid: make([]uint64, 1)}

	err = cs.AddCandle(99, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before set start")
	assert.Equal(t, 1, cs.outOfRange)

	err = cs.AddCandle(101, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not aligned")

	err = cs.AddCandle(100+2*Timestamp(M1), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
	assert.Equal(t, 2, cs.outOfRange)

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.True(t, cs.IsValid(0))

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.Equal(t, 1, cs.duplicates)

	badTF := &candleSet{Start: 100, Timeframe: 0, Candles: make([]Candle, 1), Valid: make([]uint64, 1)}
	err = badTF.AddCandle(100, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")
}

func TestCandleSetMerge_SuccessAndValidationErrors(t *testing.T) {
	t.Parallel()

	monthStart := FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	dst, err := newMonthlyCandleSet("EURUSD", M1, monthStart, PriceScale, SourceCandles)
	require.NoError(t, err)
	src, err := newMonthlyCandleSet("EURUSD", M1, monthStart, PriceScale, SourceCandles)
	require.NoError(t, err)

	t0 := Timestamp(monthStart)
	t1 := t0 + 60
	require.NoError(t, src.AddCandle(t0, Candle{Open: 100, High: 120, Low: 90, Close: 110, Ticks: 10}))
	require.NoError(t, src.AddCandle(t1, Candle{Open: 111, High: 130, Low: 105, Close: 125, Ticks: 8}))

	require.NoError(t, dst.Merge(src))
	assert.Equal(t, 2, dst.CountValid())
	assert.Equal(t, Candle{Open: 100, High: 120, Low: 90, Close: 110, Ticks: 10}, dst.Candles[0])
	assert.Equal(t, Candle{Open: 111, High: 130, Low: 105, Close: 125, Ticks: 8}, dst.Candles[1])

	differentTF, err := newMonthlyCandleSet("EURUSD", H1, monthStart, PriceScale, SourceCandles)
	require.NoError(t, err)
	require.ErrorContains(t, dst.Merge(differentTF), "timeframe mismatch")

	differentInst, err := newMonthlyCandleSet("USDJPY", M1, monthStart, PriceScale, SourceCandles)
	require.NoError(t, err)
	require.NoError(t, differentInst.AddCandle(t0, Candle{Open: 200, High: 220, Low: 180, Close: 210, Ticks: 5}))
	require.ErrorContains(t, dst.Merge(differentInst), "instrument mismatch")
}

func TestCandleSetBuildGapReportAndStats(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()) // Friday
	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  M1,
		Scale:      PriceScale,
		Source:     SourceCandles,
		Candles:    make([]Candle, 1500),
		Valid:      make([]uint64, (1500+63)/64),
	}
	bitSet(cs.Valid, 0)
	bitSet(cs.Valid, 6)
	bitSet(cs.Valid, 17)
	bitSet(cs.Valid, 1458)

	cs.BuildGapReport()
	require.Len(t, cs.Gaps, 4)
	assert.Equal(t, int32(1), cs.Gaps[0].StartIdx)
	assert.Equal(t, int32(5), cs.Gaps[0].Len)
	assert.Equal(t, "minor", cs.Gaps[0].Kind)
	assert.Equal(t, "suspicious", cs.Gaps[1].Kind)
	assert.Equal(t, "weekend", cs.Gaps[2].Kind)
	assert.Equal(t, "suspicious", cs.Gaps[3].Kind)

	s := cs.Stats()
	assert.Equal(t, 1500, s.TotalBars)
	assert.Equal(t, 4, s.PresentBars)
	assert.Equal(t, 1496, s.MissingBars)
	assert.Equal(t, 4, s.GapCount)
	assert.Equal(t, 1, s.WeekendGaps)
	assert.Equal(t, 2, s.SuspiciousGaps)
	assert.Equal(t, 1440, s.LongestGapBars)
	assert.Equal(t, "weekend", s.LongestGapKind)
}

func TestCandleSetClassifyGap_LongSuspiciousNonWeekendStart(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC).Unix()) // Monday
	cs := &candleSet{Start: start, Timeframe: M1}
	assert.Equal(t, "suspicious", cs.classifyGap(0, 24*60))
}

func TestCandleSetAggregateH1_ThresholdAndOHLC(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())
	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  M1,
		Scale:      PriceScale,
		Source:     SourceCandles,
		Candles:    make([]Candle, 120),
		Valid:      make([]uint64, (120+63)/64),
	}
	for i := 0; i < 60; i++ {
		cs.Candles[i] = Candle{Open: Price(1000 + i), High: Price(1100 + i), Low: Price(900 - i), Close: Price(1050 + i)}
		bitSet(cs.Valid, i)
	}
	for i := 60; i < 109; i++ {
		cs.Candles[i] = Candle{Open: 2000, High: 2100, Low: 1900, Close: 2050}
		bitSet(cs.Valid, i)
	}

	h1, err := cs.AggregateH1(50)
	require.NoError(t, err)
	require.Len(t, h1.Candles, 2)
	assert.True(t, h1.IsValid(0))
	assert.False(t, h1.IsValid(1))
	assert.Equal(t, Candle{Open: 1000, High: 1159, Low: 841, Close: 1109}, h1.Candles[0])

	withClamp, err := cs.AggregateH1(0)
	require.NoError(t, err)
	assert.True(t, withClamp.IsValid(0))
	assert.True(t, withClamp.IsValid(1))
}

func TestCandleSetAggregateH1_ErrorForNonM1(t *testing.T) {
	t.Parallel()

	cs := &candleSet{Timeframe: H1}
	_, err := cs.AggregateH1(10)
	assert.Error(t, err)
}

func TestCandleFormattingHelpers(t *testing.T) {
	t.Parallel()

	c := Candle{Open: 1, High: 2, Low: 3, Close: 4, AvgSpread: 5, MaxSpread: 6, Ticks: 7}
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004", c.String())
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004: avg spread 0.00005, max spread 0.00006, ticks: 7", c.FullString())

	ct := candleTime{Candle: c, Timestamp: Timestamp(100)}
	assert.Equal(t, c.String(), ct.String())
	assert.Equal(t, c.String(), fmt.Sprint(ct))
}

func TestCandleSetFilenameTimeAndBitHelpers(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC).Unix())
	h1 := &candleSet{Instrument: "EURUSD", Start: start, Timeframe: H1}
	d1 := &candleSet{Instrument: "EURUSD", Start: start, Timeframe: D1}

	assert.Equal(t, "eurusd-h1-2026", h1.Filename())
	assert.Equal(t, "eurusd-d1-all", d1.Filename())
	assert.Equal(t, time.Date(2026, time.March, 1, 1, 0, 0, 0, time.UTC), h1.Time(1))

	valid := make([]uint64, 2)
	bitSet(valid, 5)
	bitSet(valid, 70)
	assert.True(t, bitIsSet(valid, 5))
	assert.True(t, bitIsSet(valid, 70))
	assert.False(t, bitIsSet(valid, 6))
}

func TestCandleSetPrintStatsAndConversions(t *testing.T) {
	t.Parallel()

	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()),
		Timeframe:  M1,
		Scale:      PriceScale,
		Candles:    make([]Candle, 20),
		Valid:      make([]uint64, 1),
	}
	bitSet(cs.Valid, 0)
	bitSet(cs.Valid, 12)

	buf := &bufferWriteCloser{}
	cs.PrintStats(buf)
	out := buf.String()
	assert.Contains(t, out, "CandleSet Stats")
	assert.Contains(t, out, "Timeframe: m1")
	assert.Contains(t, out, "Total Bars: 20")
	assert.Contains(t, out, "Present Bars: 2")
	assert.Contains(t, out, "Missing Bars: 18")
	assert.Contains(t, out, "Longest Gap")
}

func TestCandleSetIteratorAccessors(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())
	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  H1,
		Scale:      PriceScale,
		Candles: []Candle{
			{Open: 10, High: 20, Low: 5, Close: 15, Ticks: 1},
			{Open: 30, High: 40, Low: 25, Close: 35, Ticks: 2},
		},
		Valid: make([]uint64, 1),
	}
	cs.SetValid(0)
	cs.SetValid(1)

	it := cs.Iterator()
	first, ok := it.NextCandle()
	require.True(t, ok)
	assert.Equal(t, cs.Candles[0], first)
	assert.Equal(t, 0, it.Index())
	assert.Equal(t, start, it.StartTime())
	assert.Equal(t, start, it.Timestamp())
	assert.Equal(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), it.Time())

	second, ok := it.NextCandle()
	require.True(t, ok)
	assert.Equal(t, cs.Candles[1], second)
	assert.Equal(t, 1, it.Index())

	_, ok = it.NextCandle()
	assert.False(t, ok)
}
