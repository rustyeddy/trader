package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sessionCT(ts time.Time) CandleTime {
	return CandleTime{
		Candle:    Candle{Open: 10000, High: 10100, Low: 9900, Close: 10050},
		Timestamp: FromTime(ts),
	}
}

func TestSessionFilter_NotReadyBeforeFirstTick(t *testing.T) {
	t.Parallel()
	f := NewSessionFilter(7, 17)
	assert.False(t, f.Ready())
	assert.True(t, f.Trending(), "allow entries before first tick (warmup)")
}

func TestSessionFilter_ReadyAfterFirstTick(t *testing.T) {
	t.Parallel()
	f := NewSessionFilter(7, 17)
	f.Tick(sessionCT(time.Date(2024, 1, 2, 8, 0, 0, 0, time.UTC)))
	assert.True(t, f.Ready())
}

func TestSessionFilter_AllowsEntryInsideWindow(t *testing.T) {
	t.Parallel()
	f := NewSessionFilter(7, 17)

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
	f := NewSessionFilter(7, 17)

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
	f := NewSessionFilter(7, 17)

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
	f := NewSessionFilter(13, 22)

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
	f := NewSessionFilter(7, 17)
	assert.Equal(t, "Session(07:00-17:00UTC)", f.Name())
}

func TestSessionFilter_AllowSideAlwaysTrue(t *testing.T) {
	t.Parallel()
	f := NewSessionFilter(7, 17)
	assert.True(t, f.AllowSide(Long))
	assert.True(t, f.AllowSide(Short))
}
