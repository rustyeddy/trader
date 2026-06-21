package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── splitCSV ──────────────────────────────────────────────────────────────────

func TestSplitCSV_Empty(t *testing.T) {
	assert.Empty(t, splitCSV(""))
}

func TestSplitCSV_Single(t *testing.T) {
	assert.Equal(t, []string{"EURUSD"}, splitCSV("EURUSD"))
}

func TestSplitCSV_Multiple(t *testing.T) {
	assert.Equal(t, []string{"EURUSD", "USDJPY", "GBPUSD"}, splitCSV("EURUSD,USDJPY,GBPUSD"))
}

func TestSplitCSV_TrimsWhitespace(t *testing.T) {
	assert.Equal(t, []string{"EURUSD", "USDJPY"}, splitCSV("  EURUSD , USDJPY "))
}

func TestSplitCSV_FiltersEmptyEntries(t *testing.T) {
	assert.Equal(t, []string{"EURUSD", "USDJPY"}, splitCSV("EURUSD,,USDJPY,"))
}

// ── parseMonthStart ───────────────────────────────────────────────────────────

func TestParseMonthStart_Valid(t *testing.T) {
	ts, err := parseMonthStart("2024-03")
	require.NoError(t, err)
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, 3, int(ts.Month()))
	assert.Equal(t, 1, ts.Day())
	assert.True(t, ts.Location().String() == "UTC")
}

func TestParseMonthStart_TrimsWhitespace(t *testing.T) {
	ts, err := parseMonthStart("  2024-01  ")
	require.NoError(t, err)
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, 1, int(ts.Month()))
}

func TestParseMonthStart_InvalidFormat(t *testing.T) {
	_, err := parseMonthStart("2024-13")
	require.Error(t, err)
}

func TestParseMonthStart_WrongFormat(t *testing.T) {
	_, err := parseMonthStart("2024-01-01")
	require.Error(t, err)
}

// ── parseMonthEndExclusive ────────────────────────────────────────────────────

func TestParseMonthEndExclusive_ReturnsFirstOfNextMonth(t *testing.T) {
	ts, err := parseMonthEndExclusive("2024-03")
	require.NoError(t, err)
	// Should return 2024-04-01 (first day of next month — exclusive upper bound).
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, 4, int(ts.Month()))
	assert.Equal(t, 1, ts.Day())
}

func TestParseMonthEndExclusive_December(t *testing.T) {
	ts, err := parseMonthEndExclusive("2024-12")
	require.NoError(t, err)
	assert.Equal(t, 2025, ts.Year())
	assert.Equal(t, 1, int(ts.Month()))
	assert.Equal(t, 1, ts.Day())
}

func TestParseMonthEndExclusive_InvalidFormat(t *testing.T) {
	_, err := parseMonthEndExclusive("not-a-month")
	require.Error(t, err)
}

// ── sync/download-ticks/build-candles commands ────────────────────────────────

func TestNewSyncCmd_FlagsMissingReturnsError(t *testing.T) {
	cmd := newSyncCmd(nil)
	// --instruments, --from, --to are all required.
	err := cmd.Execute()
	require.Error(t, err)
}

func TestNewSyncLikeCmd_EmptyInstrumentsError(t *testing.T) {
	cmd := newSyncLikeCmd(nil, "sync", "sync", true, true)
	_ = cmd.Flags().Set("instruments", "")
	_ = cmd.Flags().Set("from", "2024-01")
	_ = cmd.Flags().Set("to", "2024-03")
	// Mark required flags as set to bypass cobra's validation.
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing --instruments")
}

func TestNewSyncLikeCmd_BadFromReturnsError(t *testing.T) {
	cmd := newSyncLikeCmd(nil, "sync", "sync", true, true)
	_ = cmd.Flags().Set("instruments", "EURUSD")
	_ = cmd.Flags().Set("from", "not-a-month")
	_ = cmd.Flags().Set("to", "2024-03")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from")
}

func TestNewSyncLikeCmd_BadToReturnsError(t *testing.T) {
	cmd := newSyncLikeCmd(nil, "sync", "sync", true, true)
	_ = cmd.Flags().Set("instruments", "EURUSD")
	_ = cmd.Flags().Set("from", "2024-01")
	_ = cmd.Flags().Set("to", "bad-month")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--to")
}

func TestNewSyncLikeCmd_FromGteToReturnsError(t *testing.T) {
	cmd := newSyncLikeCmd(nil, "sync", "sync", true, true)
	_ = cmd.Flags().Set("instruments", "EURUSD")
	_ = cmd.Flags().Set("from", "2024-06")
	_ = cmd.Flags().Set("to", "2024-01")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from must be before --to")
}
