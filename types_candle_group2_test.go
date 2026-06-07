package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCandleSetMerge_SuccessAndValidationErrors verifies expected behavior for this component.
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

// TestCandleSetBuildGapReportAndStats verifies expected behavior for this component.
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

	// Valid candles that split three gaps: minor(5), suspicious(10), weekend(1440)
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
	assert.Equal(t, 1500, s.TotalMinutes)
	assert.Equal(t, 4, s.PresentMinutes)
	assert.Equal(t, 1496, s.MissingMinutes)
	assert.Equal(t, 4, s.GapCount)
	assert.Equal(t, 1, s.WeekendGaps)
	assert.Equal(t, 2, s.SuspiciousGaps)
	assert.Equal(t, 1440, s.LongestGap)
	assert.Equal(t, "weekend", s.LongestGapKind)
}

// TestCandleSetClassifyGap_LongSuspiciousNonWeekendStart verifies expected behavior for this component.
func TestCandleSetClassifyGap_LongSuspiciousNonWeekendStart(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC).Unix()) // Monday
	cs := &candleSet{Start: start, Timeframe: M1}

	kind := cs.classifyGap(0, 24*60)
	assert.Equal(t, "suspicious", kind)
}

// TestCandleSetAggregateH1_ThresholdAndOHLC verifies expected behavior for this component.
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
		cs.Candles[i] = Candle{
			Open:  Price(1000 + i),
			High:  Price(1100 + i),
			Low:   Price(900 - i),
			Close: Price(1050 + i),
		}
		bitSet(cs.Valid, i)
	}

	for i := 60; i < 109; i++ { // 49 valid candles in second hour
		cs.Candles[i] = Candle{Open: 2000, High: 2100, Low: 1900, Close: 2050}
		bitSet(cs.Valid, i)
	}

	h1, err := cs.AggregateH1(50)
	require.NoError(t, err)
	require.Len(t, h1.Candles, 2)
	assert.True(t, h1.IsValid(0))
	assert.False(t, h1.IsValid(1))
	assert.Equal(t, Candle{Open: 1000, High: 1159, Low: 841, Close: 1109}, h1.Candles[0])

	withClamp, err := cs.AggregateH1(0) // minValid should clamp to 1
	require.NoError(t, err)
	assert.True(t, withClamp.IsValid(0))
	assert.True(t, withClamp.IsValid(1))
}

// TestCandleSetAggregateH1_ErrorForNonM1 verifies that AggregateH1 returns an error for non-M1 input.
func TestCandleSetAggregateH1_ErrorForNonM1(t *testing.T) {
	t.Parallel()

	cs := &candleSet{Timeframe: H1}
	_, err := cs.AggregateH1(10)
	assert.Error(t, err)
}
