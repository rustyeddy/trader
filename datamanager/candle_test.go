package datamanager

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bufferWriteCloser is a test helper that wraps bytes.Buffer as a WriteCloser.
type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error { return nil }

// loadCandleSet loads the shared M1 test dataset, skipping if it is absent.
func loadCandleSet(t *testing.T) *CandleSet {
	t.Helper()
	fname := "./testdata/DAT_ASCII_EURUSD_M1_2025.csv"
	if _, err := os.Stat(fname); err != nil {
		t.Skip("candle test dataset missing")
	}
	return &CandleSet{Filepath: fname}
}

// --- file-based tests (skip when testdata is absent) ---

func TestIterator(t *testing.T) {
	cs := loadCandleSet(t)

	expected := market.Candle{Open: 1035030, High: 1035140, Low: 1035030, Close: 1035140}
	it := cs.Iterator()
	it.Next()
	assert.Equal(t, market.Timestamp(1735768800), it.StartTime())
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

	monthStart := market.Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())

	_, err := NewMonthlyCandleSet("", market.M1, monthStart, market.PriceScale, market.SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")

	_, err = NewMonthlyCandleSet("EURUSD", market.TF0, monthStart, market.PriceScale, market.SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")

	badMinute := market.Timestamp(time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC).Unix())
	_, err = NewMonthlyCandleSet("EURUSD", market.M1, badMinute, market.PriceScale, market.SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minute boundary")

	badMonthStart := market.Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix())
	_, err = NewMonthlyCandleSet("EURUSD", market.M1, badMonthStart, market.PriceScale, market.SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start of month")
}

func TestCandleSetAddCandle_Branches(t *testing.T) {
	t.Parallel()

	c := market.Candle{Open: 1, High: 2, Low: 1, Close: 2, Ticks: 1}

	var nilSet *CandleSet
	err := nilSet.AddCandle(1, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil CandleSet")

	cs := &CandleSet{Start: 100, Timeframe: market.M1, Candles: make([]market.Candle, 2), Valid: make([]uint64, 1)}

	err = cs.AddCandle(99, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before set start")
	assert.Equal(t, 1, cs.outOfRange)

	err = cs.AddCandle(101, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not aligned")

	err = cs.AddCandle(100+2*market.Timestamp(market.M1), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
	assert.Equal(t, 2, cs.outOfRange)

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.True(t, cs.IsValid(0))

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.Equal(t, 1, cs.duplicates)

	badTF := &CandleSet{Start: 100, Timeframe: 0, Candles: make([]market.Candle, 1), Valid: make([]uint64, 1)}
	err = badTF.AddCandle(100, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")
}

func TestCandleSetMerge_SuccessAndValidationErrors(t *testing.T) {
	t.Parallel()

	monthStart := market.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	dst, err := NewMonthlyCandleSet("EURUSD", market.M1, monthStart, market.PriceScale, market.SourceCandles)
	require.NoError(t, err)
	src, err := NewMonthlyCandleSet("EURUSD", market.M1, monthStart, market.PriceScale, market.SourceCandles)
	require.NoError(t, err)

	t0 := market.Timestamp(monthStart)
	t1 := t0 + 60
	require.NoError(t, src.AddCandle(t0, market.Candle{Open: 100, High: 120, Low: 90, Close: 110, Ticks: 10}))
	require.NoError(t, src.AddCandle(t1, market.Candle{Open: 111, High: 130, Low: 105, Close: 125, Ticks: 8}))

	require.NoError(t, dst.Merge(src))
	assert.Equal(t, 2, dst.CountValid())
	assert.Equal(t, market.Candle{Open: 100, High: 120, Low: 90, Close: 110, Ticks: 10}, dst.Candles[0])
	assert.Equal(t, market.Candle{Open: 111, High: 130, Low: 105, Close: 125, Ticks: 8}, dst.Candles[1])

	differentTF, err := NewMonthlyCandleSet("EURUSD", market.H1, monthStart, market.PriceScale, market.SourceCandles)
	require.NoError(t, err)
	require.ErrorContains(t, dst.Merge(differentTF), "timeframe mismatch")

	differentInst, err := NewMonthlyCandleSet("USDJPY", market.M1, monthStart, market.PriceScale, market.SourceCandles)
	require.NoError(t, err)
	require.NoError(t, differentInst.AddCandle(t0, market.Candle{Open: 200, High: 220, Low: 180, Close: 210, Ticks: 5}))
	require.ErrorContains(t, dst.Merge(differentInst), "instrument mismatch")
}

func TestCandleSetBuildGapReportAndStats(t *testing.T) {
	t.Parallel()

	start := market.Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()) // Friday
	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  market.M1,
		Scale:      market.PriceScale,
		Source:     market.SourceCandles,
		Candles:    make([]market.Candle, 1500),
		Valid:      make([]uint64, (1500+63)/64),
	}
	market.BitSet(cs.Valid, 0)
	market.BitSet(cs.Valid, 6)
	market.BitSet(cs.Valid, 17)
	market.BitSet(cs.Valid, 1458)

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

	start := market.Timestamp(time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC).Unix()) // Monday
	cs := &CandleSet{Start: start, Timeframe: market.M1}
	assert.Equal(t, "suspicious", cs.classifyGap(0, 24*60))
}

func TestCandleSetAggregateH1_ThresholdAndOHLC(t *testing.T) {
	t.Parallel()

	start := market.Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())
	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  market.M1,
		Scale:      market.PriceScale,
		Source:     market.SourceCandles,
		Candles:    make([]market.Candle, 120),
		Valid:      make([]uint64, (120+63)/64),
	}
	for i := 0; i < 60; i++ {
		cs.Candles[i] = market.Candle{Open: market.Price(1000 + i), High: market.Price(1100 + i), Low: market.Price(900 - i), Close: market.Price(1050 + i)}
		market.BitSet(cs.Valid, i)
	}
	for i := 60; i < 109; i++ {
		cs.Candles[i] = market.Candle{Open: 2000, High: 2100, Low: 1900, Close: 2050}
		market.BitSet(cs.Valid, i)
	}

	h1, err := cs.AggregateH1(50)
	require.NoError(t, err)
	require.Len(t, h1.Candles, 2)
	assert.True(t, h1.IsValid(0))
	assert.False(t, h1.IsValid(1))
	assert.Equal(t, market.Candle{Open: 1000, High: 1159, Low: 841, Close: 1109}, h1.Candles[0])

	withClamp, err := cs.AggregateH1(0)
	require.NoError(t, err)
	assert.True(t, withClamp.IsValid(0))
	assert.True(t, withClamp.IsValid(1))
}

func TestCandleSetAggregateH1_ErrorForNonM1(t *testing.T) {
	t.Parallel()

	cs := &CandleSet{Timeframe: market.H1}
	_, err := cs.AggregateH1(10)
	assert.Error(t, err)
}

func TestCandleFormattingHelpers(t *testing.T) {
	t.Parallel()

	c := market.Candle{Open: 1, High: 2, Low: 3, Close: 4, AvgSpread: 5, MaxSpread: 6, Ticks: 7}
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004", c.String())
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004: avg spread 0.00005, max spread 0.00006, ticks: 7", c.FullString())

	ct := market.CandleTime{Candle: c, Timestamp: market.Timestamp(100)}
	assert.Equal(t, c.String(), ct.String())
	assert.Equal(t, c.String(), fmt.Sprint(ct))
}

func TestCandleSetFilenameTimeAndBitHelpers(t *testing.T) {
	t.Parallel()

	start := market.Timestamp(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC).Unix())
	h1 := &CandleSet{Instrument: "EURUSD", Start: start, Timeframe: market.H1}
	d1 := &CandleSet{Instrument: "EURUSD", Start: start, Timeframe: market.D1}

	assert.Equal(t, "eurusd-h1-2026", h1.Filename())
	assert.Equal(t, "eurusd-d1-all", d1.Filename())
	assert.Equal(t, time.Date(2026, time.March, 1, 1, 0, 0, 0, time.UTC), h1.Time(1))

	valid := make([]uint64, 2)
	market.BitSet(valid, 5)
	market.BitSet(valid, 70)
	assert.True(t, market.BitIsSet(valid, 5))
	assert.True(t, market.BitIsSet(valid, 70))
	assert.False(t, market.BitIsSet(valid, 6))
}

func TestCandleSetPrintStatsAndConversions(t *testing.T) {
	t.Parallel()

	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      market.Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()),
		Timeframe:  market.M1,
		Scale:      market.PriceScale,
		Candles:    make([]market.Candle, 20),
		Valid:      make([]uint64, 1),
	}
	market.BitSet(cs.Valid, 0)
	market.BitSet(cs.Valid, 12)

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

	start := market.Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())
	cs := &CandleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  market.H1,
		Scale:      market.PriceScale,
		Candles: []market.Candle{
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
