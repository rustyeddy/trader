package data

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	trader "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── monthInRange ──────────────────────────────────────────────────────────────

func TestMonthInRange_BeforeStartYearExcluded(t *testing.T) {
	assert.False(t, monthInRange(2023, 12, 2024, 2024, time.January, time.December))
}

func TestMonthInRange_AfterEndYearExcluded(t *testing.T) {
	assert.False(t, monthInRange(2025, 1, 2024, 2024, time.January, time.December))
}

func TestMonthInRange_BeforeStartMonthInStartYearExcluded(t *testing.T) {
	assert.False(t, monthInRange(2024, 2, 2024, 2025, time.March, time.December))
}

func TestMonthInRange_AfterEndMonthInEndYearExcluded(t *testing.T) {
	assert.False(t, monthInRange(2025, 6, 2024, 2025, time.January, time.May))
}

func TestMonthInRange_ExactStartIncluded(t *testing.T) {
	assert.True(t, monthInRange(2024, 3, 2024, 2025, time.March, time.December))
}

func TestMonthInRange_ExactEndIncluded(t *testing.T) {
	assert.True(t, monthInRange(2025, 5, 2024, 2025, time.January, time.May))
}

func TestMonthInRange_MidYearIncluded(t *testing.T) {
	assert.True(t, monthInRange(2024, 7, 2024, 2024, time.January, time.December))
}

func TestMonthInRange_SingleMonthRange(t *testing.T) {
	assert.True(t, monthInRange(2024, 6, 2024, 2024, time.June, time.June))
	assert.False(t, monthInRange(2024, 5, 2024, 2024, time.June, time.June))
	assert.False(t, monthInRange(2024, 7, 2024, 2024, time.June, time.June))
}

// ── maxInstrumentLen ──────────────────────────────────────────────────────────

func TestMaxInstrumentLen_Empty(t *testing.T) {
	assert.Equal(t, 0, maxInstrumentLen(nil))
}

func TestMaxInstrumentLen_Single(t *testing.T) {
	assert.Equal(t, 6, maxInstrumentLen([]string{"EURUSD"}))
}

func TestMaxInstrumentLen_Multiple(t *testing.T) {
	assert.Equal(t, 8, maxInstrumentLen([]string{"EURUSD", "AUDUSD", "USDCHF01"}))
}

func TestMaxInstrumentLen_AllSameLength(t *testing.T) {
	assert.Equal(t, 6, maxInstrumentLen([]string{"EURUSD", "USDJPY", "GBPUSD"}))
}

// ── writeValidationReport ─────────────────────────────────────────────────────

func TestWriteValidationReport_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	payload := map[string]any{
		"instrument": "EURUSD",
		"issues":     []string{"missing month"},
	}
	require.NoError(t, writeValidationReport(path, payload))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, "EURUSD", out["instrument"])
}

func TestWriteValidationReport_BadPathReturnsError(t *testing.T) {
	err := writeValidationReport("/nonexistent/dir/report.json", map[string]any{})
	require.Error(t, err)
}

// ── printValidationGrid (smoke test) ─────────────────────────────────────────

func TestPrintValidationGrid_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	report := &trader.CandleValidationReport{}
	require.NotPanics(t, func() {
		printValidationGrid(cmd,
			[]string{"EURUSD", "USDJPY"},
			2024, 2024,
			time.January, time.December,
			report,
		)
	})
	out := buf.String()
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "USDJPY")
}

func TestPrintValidationGrid_MarksIssues(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	report := &trader.CandleValidationReport{
		Issues: []trader.CandleValidationIssue{
			{Instrument: "EURUSD", Year: 2024, Month: 6, Severity: "warn", Message: "missing"},
		},
	}
	require.NotPanics(t, func() {
		printValidationGrid(cmd,
			[]string{"EURUSD"},
			2024, 2024,
			time.January, time.December,
			report,
		)
	})
	// Month 6 has an issue so it should show '!' in the output.
	assert.Contains(t, buf.String(), "!")
}
