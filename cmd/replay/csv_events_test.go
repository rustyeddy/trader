package replay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── parseFloat ────────────────────────────────────────────────────────────────

func TestParseFloat_Valid(t *testing.T) {
	v, err := parseFloat("1.0850")
	require.NoError(t, err)
	assert.InDelta(t, 1.085, v, 1e-9)
}

func TestParseFloat_WithLeadingTrailingSpaces(t *testing.T) {
	v, err := parseFloat("  1.09  ")
	require.NoError(t, err)
	assert.InDelta(t, 1.09, v, 1e-9)
}

func TestParseFloat_Invalid(t *testing.T) {
	_, err := parseFloat("not-a-number")
	require.Error(t, err)
}

// ── inRange ───────────────────────────────────────────────────────────────────

func ts(sec int64) trader.Timestamp { return trader.Timestamp(sec) }

func TestInRange_ZeroBoundsAlwaysTrue(t *testing.T) {
	assert.True(t, inRange(ts(1000), ts(0), ts(0)))
}

func TestInRange_BeforeFromExcluded(t *testing.T) {
	assert.False(t, inRange(ts(50), ts(100), ts(0)))
}

func TestInRange_AtFromIncluded(t *testing.T) {
	assert.True(t, inRange(ts(100), ts(100), ts(0)))
}

func TestInRange_AtToExcluded(t *testing.T) {
	assert.False(t, inRange(ts(200), ts(0), ts(200)))
}

func TestInRange_BeforeToIncluded(t *testing.T) {
	assert.True(t, inRange(ts(199), ts(0), ts(200)))
}

func TestInRange_WithinBothBounds(t *testing.T) {
	assert.True(t, inRange(ts(150), ts(100), ts(200)))
}

// ── parseTickRowCompat ────────────────────────────────────────────────────────

func row(cols ...string) []string { return cols }

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestParseTickRowCompat_ValidRow(t *testing.T) {
	now := rfc3339(time.Now())
	tick, ok, err := parseTickRowCompat(row(now, "EURUSD", "1.0800", "1.0802"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "EURUSD", tick.Instrument)
	assert.Greater(t, int64(tick.BA.Ask), int64(tick.BA.Bid))
}

func TestParseTickRowCompat_EmptyTimeSkips(t *testing.T) {
	_, ok, err := parseTickRowCompat(row("", "EURUSD", "1.08", "1.09"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestParseTickRowCompat_EmptyInstrumentSkips(t *testing.T) {
	now := rfc3339(time.Now())
	_, ok, err := parseTickRowCompat(row(now, "", "1.08", "1.09"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestParseTickRowCompat_BadTimeReturnsError(t *testing.T) {
	_, _, err := parseTickRowCompat(row("not-a-time", "EURUSD", "1.08", "1.09"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad time")
}

func TestParseTickRowCompat_BadBidReturnsError(t *testing.T) {
	now := rfc3339(time.Now())
	_, _, err := parseTickRowCompat(row(now, "EURUSD", "bad", "1.09"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bid")
}

func TestParseTickRowCompat_BadAskReturnsError(t *testing.T) {
	now := rfc3339(time.Now())
	_, _, err := parseTickRowCompat(row(now, "EURUSD", "1.08", "bad"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ask")
}

func TestParseTickRowCompat_AskLessThanBidReturnsError(t *testing.T) {
	now := rfc3339(time.Now())
	_, _, err := parseTickRowCompat(row(now, "EURUSD", "1.0900", "1.0800"))
	require.Error(t, err)
}

// ── CSVEventsFeed ─────────────────────────────────────────────────────────────

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ticks.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestCSVEventsFeed_EmptyFile(t *testing.T) {
	path := writeCSV(t, "")
	feed, err := NewCSVEventsFeed(path, 0, 0)
	require.NoError(t, err)
	defer feed.Close()

	_, ok, err := feed.Next()
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCSVEventsFeed_SkipsHeader(t *testing.T) {
	now := rfc3339(time.Now())
	csv := "time,instrument,bid,ask\n" + now + ",EURUSD,1.0800,1.0802\n"
	feed, err := NewCSVEventsFeed(writeCSV(t, csv), 0, 0)
	require.NoError(t, err)
	defer feed.Close()

	row, ok, err := feed.Next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "EURUSD", row.Tick.Instrument)
	assert.Empty(t, row.Event)
}

func TestCSVEventsFeed_ParsesEventColumns(t *testing.T) {
	now := rfc3339(time.Now())
	csv := now + ",EURUSD,1.0800,1.0802,OPEN,1000,20,,\n"
	feed, err := NewCSVEventsFeed(writeCSV(t, csv), 0, 0)
	require.NoError(t, err)
	defer feed.Close()

	row, ok, err := feed.Next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "OPEN", row.Event)
	assert.Equal(t, "1000", row.P1)
	assert.Equal(t, "20", row.P2)
	assert.Equal(t, "", row.P3)
	assert.Equal(t, "", row.P4)
}

func TestCSVEventsFeed_SkipsRowsOutsideRange(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	csv := rfc3339(t1) + ",EURUSD,1.08,1.09\n" +
		rfc3339(t2) + ",EURUSD,1.08,1.09\n" +
		rfc3339(t3) + ",EURUSD,1.08,1.09\n"

	from := trader.FromTime(t2)
	to := trader.FromTime(t3)
	feed, err := NewCSVEventsFeed(writeCSV(t, csv), from, to)
	require.NoError(t, err)
	defer feed.Close()

	ev, ok, err := feed.Next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, from, ev.Tick.Timestamp)

	_, ok, err = feed.Next()
	require.NoError(t, err)
	assert.False(t, ok, "row at t3 (== to) should be excluded")
}

func TestCSVEventsFeed_TooManyColumnsReturnsError(t *testing.T) {
	now := rfc3339(time.Now())
	// 10 cols (one too many — max is 9)
	csv := now + ",EURUSD,1.08,1.09,EVENT,p1,p2,p3,p4,extra\n"
	feed, err := NewCSVEventsFeed(writeCSV(t, csv), 0, 0)
	require.NoError(t, err)
	defer feed.Close()

	_, _, err = feed.Next()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many columns")
}

func TestCSVEventsFeed_MissingFileReturnsError(t *testing.T) {
	_, err := NewCSVEventsFeed("/nonexistent/file.csv", 0, 0)
	require.Error(t, err)
}

func TestCSVEventsFeed_MultipleRows(t *testing.T) {
	t1 := rfc3339(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	t2 := rfc3339(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC))
	csv := t1 + ",EURUSD,1.0800,1.0802\n" + t2 + ",USDJPY,150.00,150.02\n"
	feed, err := NewCSVEventsFeed(writeCSV(t, csv), 0, 0)
	require.NoError(t, err)
	defer feed.Close()

	r1, ok, err := feed.Next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "EURUSD", r1.Tick.Instrument)

	r2, ok, err := feed.Next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "USDJPY", r2.Tick.Instrument)

	_, ok, err = feed.Next()
	require.NoError(t, err)
	assert.False(t, ok)
}
