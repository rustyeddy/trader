package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeframeParseNormalizeAndString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, M1, TF("m1"))
	assert.Equal(t, H1, TF("H1"))
	assert.Equal(t, TF0, TF("unknown"))

	assert.Equal(t, "m1", NormalizeTF("60"))
	assert.Equal(t, "h1", NormalizeTF("3600"))
	assert.Equal(t, "M1", NormalizeTF(" m1 "))

	assert.Equal(t, "m1", M1.String())
	assert.Equal(t, "UNKNOWN", Timeframe(999).String())
}

func TestTimeRangeBasics(t *testing.T) {
	t.Parallel()

	r := NewTimeRange(100, 200, M1)
	assert.True(t, r.Valid())
	assert.True(t, r.Contains(100))
	assert.False(t, r.Contains(200))
	assert.True(t, r.Overlaps(NewTimeRange(150, 300, M1)))
	assert.True(t, r.Covers(NewTimeRange(120, 180, M1)))
}

func TestIsFXMarketClosedWrapperMatchesCanonical(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	candles := []time.Time{
		time.Date(2024, 6, 7, 16, 59, 0, 0, ny),
		time.Date(2024, 6, 7, 17, 0, 0, 0, ny),
		time.Date(2024, 6, 9, 16, 59, 0, 0, ny),
		time.Date(2024, 6, 9, 17, 0, 0, 0, ny),
		time.Date(2024, 12, 24, 13, 0, 0, 0, ny),
	}
	for _, ts := range candles {
		assert.Equal(t, IsForexMarketClosed(ts), IsFXMarketClosed(ts))
	}
}

func TestTimestampHelpers(t *testing.T) {
	t.Parallel()

	ts := Timestamp(125)
	assert.Equal(t, Timestamp(120), ts.FloorToMinute())
	assert.Equal(t, Timestamp(0), ts.FloorToHour())
	assert.Equal(t, Timemilli(125000), ts.Milli())
	assert.Equal(t, Timestamp(125), ts.MS().Sec())
}
