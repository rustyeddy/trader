package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTimeframeParseNormalizeAndString verifies expected behavior for this component.
func TestTimeframeParseNormalizeAndString(t *testing.T) {
	t.Parallel()

	tf, err := ParseTimeframe("m1")
	require.NoError(t, err)
	assert.Equal(t, M1, tf)

	tf, err = ParseTimeframe("H1")
	require.NoError(t, err)
	assert.Equal(t, H1, tf)

	tf, err = ParseTimeframe("D1")
	require.NoError(t, err)
	assert.Equal(t, D1, tf)

	tf, err = ParseTimeframe("D")
	require.NoError(t, err)
	assert.Equal(t, D1, tf) // OANDA uses "D" not "D1"

	tf, err = ParseTimeframe("d")
	require.NoError(t, err)
	assert.Equal(t, D1, tf)

	_, err = ParseTimeframe("unknown")
	require.Error(t, err)

	assert.Equal(t, "m1", normalizeTF("60"))
	assert.Equal(t, "h1", normalizeTF("3600"))
	assert.Equal(t, "m1", normalizeTF(" m1 "))

	assert.Equal(t, "m1", M1.String())
	assert.Equal(t, "timeframe(999)", Timeframe(999).String())
}

func TestParseTimeframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Timeframe
		err  bool
	}{
		{"M1", M1, false},
		{"m1", M1, false},
		{"H1", H1, false},
		{"h1", H1, false},
		{"D1", D1, false},
		{"D", D1, false},
		{"d1", D1, false},
		{"60", M1, false},
		{"3600", H1, false},
		{"86400", D1, false},
		{"tick", Ticks, false},
		{"1", Ticks, false},
		{"ticks", Ticks, false},
		{"W1", TF0, true},
		{"", TF0, true},
	}

	for _, tc := range tests {
		got, err := ParseTimeframe(tc.in)
		if tc.err {
			assert.Error(t, err, "input=%q", tc.in)
			continue
		}
		require.NoError(t, err, "input=%q", tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
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

	r := NewTimeRange(100, 200, M1)
	assert.True(t, r.Valid())
	assert.True(t, r.Contains(100))
	assert.False(t, r.Contains(200))
	assert.True(t, r.Overlaps(NewTimeRange(150, 300, M1)))
	assert.True(t, r.Covers(NewTimeRange(120, 180, M1)))
}

// TestTimestampHelpers verifies expected behavior for this component.
func TestTimestampHelpers(t *testing.T) {
	t.Parallel()

	ts := Timestamp(125)
	assert.Equal(t, Timestamp(120), ts.FloorToMinute())
	assert.Equal(t, Timestamp(0), ts.FloorToHour())
	assert.Equal(t, TimeMillis(125000), ts.Milli())
	assert.Equal(t, TimeMillis(120000), ts.Milli().FloorToMinute())
	assert.Equal(t, TimeMillis(0), ts.Milli().FloorToHour())
	assert.Equal(t, Timestamp(125), ts.MS().Sec())
	assert.True(t, Timestamp(124).Before(Timestamp(125)))
	assert.True(t, Timestamp(126).After(Timestamp(125)))
	assert.Equal(t, Timestamp(135), Timestamp(125).Add(10*time.Second))
}

// TestTimeRangeLocation verifies expected behavior for this component.
func TestTimeRangeLocation(t *testing.T) {
	t.Parallel()

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

	mr := MonthRange(2024, 2)
	assert.Equal(t, Timestamp(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.Start)
	assert.Equal(t, Timestamp(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).Unix()), mr.End)
	assert.False(t, TimeRange{}.Valid())
}
