package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTimeframeParseNormalizeAndString verifies expected behavior for this component.
func TestTimeframeParseNormalizeAndString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, M1, tfFromString("m1"))
	assert.Equal(t, H1, tfFromString("H1"))
	assert.Equal(t, D1, tfFromString("D1"))
	assert.Equal(t, D1, tfFromString("D"))  // OANDA uses "D" not "D1"
	assert.Equal(t, D1, tfFromString("d"))
	assert.Equal(t, TF0, tfFromString("unknown"))

	assert.Equal(t, "m1", normalizeTF("60"))
	assert.Equal(t, "h1", normalizeTF("3600"))
	assert.Equal(t, "M1", normalizeTF(" m1 "))

	assert.Equal(t, "m1", M1.String())
	assert.Equal(t, "UNKNOWN", Timeframe(999).String())
}

// TestParseTimeRange_OANDADailyAlias ensures "D" (OANDA's daily granularity)
// is accepted by ParseTimeRange, fixing the replay 422 on daily timeframes.
func TestParseTimeRange_OANDADailyAlias(t *testing.T) {
	t.Parallel()
	tr, err := ParseTimeRange("2024-01-01", "2024-03-01", "D")
	require.NoError(t, err)
	assert.Equal(t, D1, tr.TF)

	tr2, err := ParseTimeRange("2024-01-01", "2024-03-01", "D1")
	require.NoError(t, err)
	assert.Equal(t, D1, tr2.TF)
}

// TestTimeRangeBasics verifies expected behavior for this component.
func TestTimeRangeBasics(t *testing.T) {
	t.Parallel()

	r := newTimeRange(100, 200, M1)
	assert.True(t, r.Valid())
	assert.True(t, r.Contains(100))
	assert.False(t, r.Contains(200))
	assert.True(t, r.Overlaps(newTimeRange(150, 300, M1)))
	assert.True(t, r.Covers(newTimeRange(120, 180, M1)))
}

// TestIsFXMarketClosedWrapperMatchesCanonical verifies expected behavior for this component.
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
		assert.Equal(t, isForexMarketClosed(ts), isFXMarketClosed(ts))
	}
}

// TestTimestampHelpers verifies expected behavior for this component.
func TestTimestampHelpers(t *testing.T) {
	t.Parallel()

	ts := Timestamp(125)
	assert.Equal(t, Timestamp(120), ts.FloorToMinute())
	assert.Equal(t, Timestamp(0), ts.FloorToHour())
	assert.Equal(t, timemilli(125000), ts.Milli())
	assert.Equal(t, timemilli(120000), ts.Milli().FloorToMinute())
	assert.Equal(t, timemilli(0), ts.Milli().FloorToHour())
	assert.Equal(t, Timestamp(125), ts.MS().Sec())
	assert.True(t, Timestamp(124).Before(Timestamp(125)))
	assert.True(t, Timestamp(126).After(Timestamp(125)))
	assert.Equal(t, Timestamp(135), Timestamp(125).Add(10*time.Second))
}

// TestFromStringAndTimeRangeLocation verifies expected behavior for this component.
func TestFromStringAndTimeRangeLocation(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), FromString("2024-01-15"))
	assert.Equal(t, Timestamp(0), FromString("not-a-date"))

	loc := time.UTC
	rng, err := timeRangeLocation("2024-01-01", "2024-01-10", "H1", loc)
	require.NoError(t, err)
	assert.True(t, rng.Valid())
	assert.Equal(t, H1, rng.TF)

	_, err = timeRangeLocation("2024-01-10", "2024-01-01", "H1", loc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date range")

	_, err = timeRangeLocation("bad", "2024-01-10", "H1", loc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad from date")
}

// TestTimeRangeMonthsAndMonthRange verifies expected behavior for this component.
func TestTimeRangeMonthsAndMonthRange(t *testing.T) {
	t.Parallel()

	r := TimeRange{Start: Timestamp(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()), End: Timestamp(time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC).Unix()), TF: D1}
	months := r.MonthsInRange()
	require.Equal(t, []yearMonth{{Year: 2024, Month: 1}, {Year: 2024, Month: 2}, {Year: 2024, Month: 3}}, months)

	assert.Nil(t, TimeRange{}.MonthsInRange())

	mr := monthRange(2024, 2)
	assert.Equal(t, Timestamp(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.Start)
	assert.Equal(t, Timestamp(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.End)
	assert.False(t, TimeRange{}.Valid())
}
