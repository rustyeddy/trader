package indicators

import (
	"fmt"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// px is a price in points of 0.00001.
func px(points int64) num.Price {
	return num.MustParsePrice(fmt.Sprintf("%d.%05d", points/100000, points%100000))
}

type bar struct{ h, l, c int64 }

func feed(t *testing.T, a *ATR, bars ...bar) {
	t.Helper()
	for _, b := range bars {
		require.NoError(t, a.Update(px(b.h), px(b.l), px(b.c)))
	}
}

func TestTrueRange(t *testing.T) {
	cases := []struct {
		name             string
		h, l, prev, want int64
	}{
		{"own range is largest", 1100, 800, 1000, 300},
		{"gap up above the bar", 1500, 1400, 1000, 500},
		{"gap down below the bar", 700, 600, 1000, 400},
		{"no movement is a known zero", 1000, 1000, 1000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, px(c.want), TrueRange(px(c.h), px(c.l), px(c.prev)))
		})
	}
}

func TestNewATR_RejectsAnInvalidPeriod(t *testing.T) {
	for _, p := range []int{0, -1, maxATRPeriod + 1} {
		_, err := NewATR(p)
		assert.ErrorIs(t, err, ErrInvalidPeriod, "period %d", p)
	}
	a, err := NewATR(maxATRPeriod)
	require.NoError(t, err)
	assert.Equal(t, maxATRPeriod, a.Period())
}

func TestATR_WarmupNeedsPeriodPlusOneBars(t *testing.T) {
	a, err := NewATR(2)
	require.NoError(t, err)
	feed(t, a, bar{1000, 1000, 1000}, bar{1100, 1000, 1000})
	assert.False(t, a.Ready(), "the first bar has no previous close")
	assert.Zero(t, a.Value())

	feed(t, a, bar{1301, 1000, 1000})
	require.True(t, a.Ready())
	assert.Equal(t, 0.002005, a.Value(), "(100+301)/2 points: the half point survives, exactly")
}

func TestATR_DropsTheOldestTrueRange(t *testing.T) {
	a, err := NewATR(2)
	require.NoError(t, err)
	feed(t, a, bar{1000, 1000, 1000}, bar{1100, 1000, 1000}, bar{1301, 1000, 1000}, bar{1500, 1000, 1000})
	assert.Equal(t, 0.004005, a.Value(), "(301+500)/2, not an average of all three")
}

func TestATR_TrueRangeUsesThePreviousClose(t *testing.T) {
	for name, tc := range map[string]struct {
		b    bar
		want float64
	}{
		"own range is largest":   {bar{1100, 800, 1000}, 0.003},
		"gap up above the bar":   {bar{1500, 1400, 1450}, 0.005},
		"gap down below the bar": {bar{700, 600, 650}, 0.004},
		"no movement":            {bar{1000, 1000, 1000}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			a, err := NewATR(1)
			require.NoError(t, err)
			feed(t, a, bar{1000, 1000, 1000}, tc.b)
			require.True(t, a.Ready())
			assert.Equal(t, tc.want, a.Value())
		})
	}
}

// The value depends only on the last Period+1 bars, however much history came
// before: what distinguishes this from Wilder smoothing.
func TestATR_IsAPureFunctionOfTheLastPeriodPlusOneBars(t *testing.T) {
	var history []bar
	for i := range int64(500) {
		x := 110000 + i%37*137 + i*13
		history = append(history, bar{x + 430 + i%11*29, x - 310 - i%7*17, x + 7*(i%5)})
	}
	for _, period := range []int{1, 2, 5, 14} {
		long, err := NewATR(period)
		require.NoError(t, err)
		feed(t, long, history...)

		short, err := NewATR(period)
		require.NoError(t, err)
		feed(t, short, history[len(history)-period-1:]...)

		require.True(t, short.Ready())
		assert.Equal(t, short.Value(), long.Value(), "period %d", period)
	}
}

func TestATR_ReplayIsDeterministic(t *testing.T) {
	run := func() float64 {
		a, _ := NewATR(3)
		feed(t, a, bar{1000, 800, 900}, bar{1100, 900, 1000}, bar{1300, 1000, 1200}, bar{1200, 900, 1000}, bar{900, 700, 800})
		return a.Value()
	}
	assert.Equal(t, run(), run())
}

func TestAbove_IsTheDistanceAboveOrZero(t *testing.T) {
	assert.Equal(t, px(4), above(px(9), px(5)))
	assert.True(t, above(px(5), px(9)).IsZero(), "below is no distance above")
	assert.True(t, above(px(5), px(5)).IsZero())
}
