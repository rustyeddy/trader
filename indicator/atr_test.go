package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrueRange(t *testing.T) {
	cases := []struct {
		name                    string
		high, low, prevClose, w float64
	}{
		{"range dominates", 12, 10, 11, 2},
		{"gap up from the previous close", 12, 11, 9, 3},
		{"gap down from the previous close", 9, 8, 12, 4},
		{"flat bar", 10, 10, 10, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.InDelta(t, c.w, TrueRange(c.high, c.low, c.prevClose), 1e-12)
		})
	}
}

func TestNewATR_RejectsNonPositivePeriod(t *testing.T) {
	for _, p := range []int{0, -1} {
		_, err := NewATR(p)
		assert.ErrorIs(t, err, ErrInvalidPeriod)
	}
}

type ohlc struct{ h, l, c float64 }

var atrBars = []ohlc{
	{10, 8, 9},   // establishes the first previous close
	{11, 9, 10},  // TR 2
	{13, 10, 12}, // TR 3
	{12, 9, 10},  // TR 3
	{9, 7, 8},    // TR 3 (gap down from 10)
	{15, 8, 14},  // TR 7
}

func feed(t *testing.T, a *ATR, bars []ohlc) {
	t.Helper()
	for _, b := range bars {
		require.NoError(t, a.Update(b.h, b.l, b.c))
	}
}

func TestATR_WarmupNeedsPeriodPlusOneBars(t *testing.T) {
	a, err := NewATR(3)
	require.NoError(t, err)
	assert.Equal(t, 3, a.Period())

	for i, b := range atrBars[:3] {
		require.NoError(t, a.Update(b.h, b.l, b.c), i)
		assert.False(t, a.Ready(), "after %d bars", i+1)
		assert.Zero(t, a.Value())
	}
	require.NoError(t, a.Update(atrBars[3].h, atrBars[3].l, atrBars[3].c))
	assert.True(t, a.Ready())
	assert.InDelta(t, (2+3+3)/3.0, a.Value(), 1e-12)
}

func TestATR_RollsOverTheLastPeriodTrueRanges(t *testing.T) {
	a, err := NewATR(3)
	require.NoError(t, err)
	feed(t, a, atrBars[:5])
	assert.InDelta(t, (3+3+3)/3.0, a.Value(), 1e-12)
	feed(t, a, atrBars[5:])
	assert.InDelta(t, (3+3+7)/3.0, a.Value(), 1e-12)
}

// The value depends only on the last Period+1 bars, however much history
// came before (the property that distinguishes this from Wilder smoothing).
func TestATR_IsAPureFunctionOfTheLastPeriodPlusOneBars(t *testing.T) {
	long, err := NewATR(2)
	require.NoError(t, err)
	feed(t, long, []ohlc{{100, 1, 50}, {200, 3, 80}})
	feed(t, long, atrBars)

	short, err := NewATR(2)
	require.NoError(t, err)
	feed(t, short, atrBars[len(atrBars)-3:])

	assert.InDelta(t, short.Value(), long.Value(), 1e-9)
}

func TestATR_RejectsNonFiniteInputAndKeepsState(t *testing.T) {
	a, err := NewATR(2)
	require.NoError(t, err)
	feed(t, a, atrBars[:3])
	want := a.Value()

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		assert.ErrorIs(t, a.Update(bad, 1, 1), ErrNonFiniteSample)
		assert.ErrorIs(t, a.Update(1, bad, 1), ErrNonFiniteSample)
		assert.ErrorIs(t, a.Update(1, 1, bad), ErrNonFiniteSample)
	}
	assert.True(t, a.Ready())
	assert.Equal(t, want, a.Value())
}

func TestATR_ReplayIsDeterministic(t *testing.T) {
	run := func() float64 {
		a, _ := NewATR(3)
		feed(t, a, atrBars)
		return a.Value()
	}
	assert.Equal(t, run(), run())
}
