package strategy

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sessionCT(ts time.Time) market.Candle {
	return market.Candle{Open: 10000, High: 10100, Low: 9900, Close: 10050, Timestamp: types.FromTime(ts)}
}

func TestSessionFilter_NotReadyBeforeFirstTick(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "allow entries before first tick (warmup)")
}

func TestSessionFilter_ReadyAfterFirstTick(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)
	f.Tick(sessionCT(time.Date(2024, 1, 2, 8, 0, 0, 0, time.UTC)))
	assert.True(t, f.Ready())
}

func TestSessionFilter_AllowsEntryInsideWindow(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)

	cases := []int{7, 8, 12, 16} // hours inside [7,17)
	for _, hour := range cases {
		ts := time.Date(2024, 1, 2, hour, 0, 0, 0, time.UTC)
		f.Tick(sessionCT(ts))
		require.True(t, f.Ready())
		assert.True(t, f.Trending(), "hour %02d:00 should be inside session window", hour)
	}
}

func TestSessionFilter_BlocksEntryOutsideWindow(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)

	cases := []int{0, 1, 5, 6, 17, 18, 22, 23} // hours outside [7,17)
	for _, hour := range cases {
		ts := time.Date(2024, 1, 2, hour, 0, 0, 0, time.UTC)
		f.Tick(sessionCT(ts))
		require.True(t, f.Ready())
		assert.False(t, f.Trending(), "hour %02d:00 should be outside session window", hour)
	}
}

func TestSessionFilter_BoundaryHours(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)

	// start hour is inclusive
	f.Tick(sessionCT(time.Date(2024, 1, 2, 7, 0, 0, 0, time.UTC)))
	assert.True(t, f.Trending(), "start hour 07:00 must be included")

	// end hour is exclusive
	f.Tick(sessionCT(time.Date(2024, 1, 2, 17, 0, 0, 0, time.UTC)))
	assert.False(t, f.Trending(), "end hour 17:00 must be excluded")
}

func TestSessionFilter_CustomWindow(t *testing.T) {
	t.Parallel()
	// New York only: 13:00-22:00 UTC
	f, err := NewSessionFilter(13, 22)
	require.NoError(t, err)

	f.Tick(sessionCT(time.Date(2024, 1, 2, 13, 0, 0, 0, time.UTC)))
	assert.True(t, f.Trending())

	f.Tick(sessionCT(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)))
	assert.False(t, f.Trending())

	f.Tick(sessionCT(time.Date(2024, 1, 2, 21, 59, 0, 0, time.UTC)))
	assert.True(t, f.Trending())

	f.Tick(sessionCT(time.Date(2024, 1, 2, 22, 0, 0, 0, time.UTC)))
	assert.False(t, f.Trending())
}

func TestSessionFilter_Name(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)
	assert.Equal(t, "Session(07:00-17:00UTC)", f.Name())
}

func TestSessionFilter_AllowSideAlwaysTrue(t *testing.T) {
	t.Parallel()
	f, err := NewSessionFilter(7, 17)
	require.NoError(t, err)
	assert.True(t, f.AllowSide(types.Long))
	assert.True(t, f.AllowSide(types.Short))
}

func TestSessionFilter_RejectsInvalidWindows(t *testing.T) {
	t.Parallel()

	tests := [][2]int{
		{-1, 8},
		{24, 8},
		{7, 0},
		{7, 25},
		{17, 7},
		{7, 7},
	}

	for _, tc := range tests {
		_, err := NewSessionFilter(tc[0], tc[1])
		require.Error(t, err)
	}
}
