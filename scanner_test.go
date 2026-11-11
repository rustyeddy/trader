package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scan instruments are deliberately not in the instruments registry: a
// Scanner must not need one.
const (
	scanA = "SCANAAA"
	scanB = "SCANBBB"
)

// scanSet builds hourly bars from their opening hours and closes.
func scanSet(instrument string, hours []int, closes []price) *marketData {
	md := &marketData{instrument: instrument}
	for i, hour := range hours {
		open := mergeStart.Add(time.Duration(hour) * time.Hour)
		md.data = append(md.data, bar{
			instrument:  instrument,
			Time:        open,
			ohlc:        ohlc{c: closes[i]},
			availableAt: open.Add(time.Hour),
		})
	}
	return md
}

// available is when the bar opening at the given hour is complete.
func available(hour int) time.Time {
	return mergeStart.Add(time.Duration(hour+1) * time.Hour)
}

// scanA, with short=2 (alpha 2/3) and long=3 (alpha 1/2), in points:
//
//	hour  close   short            long    trend
//	0     100000  -                -       -
//	1     100300  100150           -       -
//	2     100600  100450           100300  up
//	3     100900  100750           100600  up
//	4     100000  100250           100300  down
//	5     99000   99416.67         99650   down
func scanASet() *marketData {
	return scanSet(scanA, []int{0, 1, 2, 3, 4, 5}, []price{100000, 100300, 100600, 100900, 100000, 99000})
}

// scanB has no bar at hour 3. The missing bar must not advance its EMAs:
//
//	hour  close   short      long    trend
//	0     200000  -          -       -
//	1     199700  199850     -       -
//	2     199400  199550     199700  down
//	4     199900  199783.33  199800  down
//
// Carrying the 199400 close through hour 3 would give short 199450 and long
// 199550 there, then 199750 and 199725 at hour 4: up, not down.
func scanBSet() *marketData {
	return scanSet(scanB, []int{0, 1, 2, 4}, []price{200000, 199700, 199400, 199900})
}

func TestRunEMAScan_ClassifiesEachInstrumentIndependently(t *testing.T) {
	got, err := runEMAScan(mergedBars(context.Background(), scanASet(), scanBSet()), 2, 3)

	require.NoError(t, err)
	assert.Equal(t, []trendReading{
		{scanA, available(2), trendUp},
		{scanB, available(2), trendDown},
		{scanA, available(3), trendUp},
		// scanB has no bar at hour 3, so no reading.
		{scanA, available(4), trendDown},
		{scanB, available(4), trendDown},
		{scanA, available(5), trendDown},
	}, got)
}

func TestRunEMAScan_OrderDoesNotDependOnTheOrderOfTheSets(t *testing.T) {
	first, err := runEMAScan(mergedBars(context.Background(), scanASet(), scanBSet()), 2, 3)
	require.NoError(t, err)
	second, err := runEMAScan(mergedBars(context.Background(), scanBSet(), scanASet()), 2, 3)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

// Each instrument's readings must be what it would get scanned alone.
func TestRunEMAScan_NoStateLeaksBetweenInstruments(t *testing.T) {
	together, err := runEMAScan(mergedBars(context.Background(), scanASet(), scanBSet()), 2, 3)
	require.NoError(t, err)

	for _, set := range []*marketData{scanASet(), scanBSet()} {
		alone, err := runEMAScan(mergedBars(context.Background(), set), 2, 3)
		require.NoError(t, err)

		var picked []trendReading
		for _, r := range together {
			if r.instrument == set.instrument {
				picked = append(picked, r)
			}
		}
		assert.Equal(t, alone, picked, set.instrument)
	}
}

func TestRunEMAScan_InstrumentsWarmUpSeparately(t *testing.T) {
	late := scanSet(scanB, []int{4, 5}, []price{200000, 200100}) // one bar short

	got, err := runEMAScan(mergedBars(context.Background(), scanASet(), late), 2, 3)

	require.NoError(t, err)
	require.Len(t, got, 4)
	for _, r := range got {
		assert.Equal(t, scanA, r.instrument, "scanB has not seen a long window")
	}
}

func TestRunEMAScan_ExactlyTheLongWindowGivesOneReading(t *testing.T) {
	set := scanSet(scanA, []int{0, 1, 2}, []price{100000, 100300, 100600})

	got, err := runEMAScan(mergedBars(context.Background(), set), 2, 3)

	require.NoError(t, err)
	assert.Equal(t, []trendReading{{scanA, available(2), trendUp}}, got)
}

func TestRunEMAScan_NoBars(t *testing.T) {
	got, err := runEMAScan(mergedBars(context.Background()), 2, 3)

	require.NoError(t, err)
	assert.Empty(t, got)
}

// 110000 points seeds the EMAs a rounding error apart: three 1.1s average to
// 1.1000000000000001. An exact comparison would call this flat market a trend.
func TestRunEMAScan_AConstantPriceIsFlat(t *testing.T) {
	set := scanSet(scanA, []int{0, 1, 2, 3, 4}, []price{110000, 110000, 110000, 110000, 110000})

	got, err := runEMAScan(mergedBars(context.Background(), set), 2, 3)

	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, trendFlat, r.trend, "at %s", r.availableAt)
	}
}

func TestRunEMAScan_ReturnsNoPartialResultsOnADataError(t *testing.T) {
	boom := errors.New("bad row")
	bars := func(yield func(bar, error) bool) {
		for _, b := range scanASet().data {
			if !yield(b, nil) {
				return
			}
		}
		yield(bar{}, boom)
	}

	got, err := runEMAScan(bars, 2, 3)

	require.ErrorIs(t, err, boom)
	assert.Nil(t, got, "the bars complete four readings before the error")
}

func TestRunEMAScan_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := runEMAScan(mergedBars(ctx, scanASet()), 2, 3)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

func TestRunEMAScan_RefusesBadWindows(t *testing.T) {
	tests := []struct {
		name        string
		short, long int
	}{
		{"zero short", 0, 3},
		{"negative short", -1, 3},
		{"short equals long", 3, 3},
		{"short above long", 4, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runEMAScan(mergedBars(context.Background(), scanASet()), tt.short, tt.long)
			require.ErrorContains(t, err, "0 < short < long")
			assert.Nil(t, got)
		})
	}
}

func TestClassifyTrend(t *testing.T) {
	tests := []struct {
		name        string
		short, long float64
		want        trend
	}{
		{"short above long", 1.1001, 1.1, trendUp},
		{"short below long", 1.1, 1.1001, trendDown},
		{"equal", 1.1, 1.1, trendFlat},
		{"rounding error above", 1.1 + 1e-12, 1.1, trendFlat},
		{"rounding error below", 1.1, 1.1 + 1e-12, trendFlat},
		{"less than a point but real, above", 1.1 + 1e-7, 1.1, trendUp},
		{"less than a point but real, below", 1.1, 1.1 + 1e-7, trendDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyTrend(tt.short, tt.long))
		})
	}
}

func TestTrend_String(t *testing.T) {
	assert.Equal(t, "up", trendUp.String())
	assert.Equal(t, "down", trendDown.String())
	assert.Equal(t, "flat", trendFlat.String())
	assert.Equal(t, "unknown", noTrend.String())
}
