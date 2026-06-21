package data

import (
	"bytes"
	"testing"
	"time"

	trader "github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── lotLabel ─────────────────────────────────────────────────────────────────

func TestLotLabel_StandardLot(t *testing.T) {
	assert.Equal(t, "standard lot", lotLabel(100_000))
}

func TestLotLabel_MiniLot(t *testing.T) {
	assert.Equal(t, "mini lot", lotLabel(10_000))
}

func TestLotLabel_MicroLot(t *testing.T) {
	assert.Equal(t, "micro lot", lotLabel(1_000))
}

func TestLotLabel_ArbitraryUnits(t *testing.T) {
	assert.Equal(t, "5000 units", lotLabel(5_000))
}

// ── printAnalysis ─────────────────────────────────────────────────────────────

func TestPrintAnalysis_NoAnalyzers_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	inst := trader.GetInstrument("EURUSD")
	require.NotNil(t, inst)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	require.NotPanics(t, func() {
		printAnalysis(&buf, inst, "EURUSD", "H1", from, to, nil, 0, 0)
	})

	out := buf.String()
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "H1")
	assert.Contains(t, out, "2024-01-01")
	assert.Contains(t, out, "2024-12-31")
}

func TestPrintAnalysis_WithAnalyzers_WritesStatNames(t *testing.T) {
	var buf bytes.Buffer
	inst := trader.GetInstrument("EURUSD")
	require.NotNil(t, inst)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)

	swing := trader.NewSwingAnalyzer(inst)
	analyzers := []trader.Analyzer{swing}

	require.NotPanics(t, func() {
		printAnalysis(&buf, inst, "EURUSD", "H1", from, to, analyzers, 0, 0)
	})

	assert.Contains(t, buf.String(), swing.Name())
}

func TestPrintAnalysis_WithUnits_ShowsUSDColumn(t *testing.T) {
	var buf bytes.Buffer
	inst := trader.GetInstrument("EURUSD")
	require.NotNil(t, inst)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	swing := trader.NewSwingAnalyzer(inst)
	// Feed one valid candle so swing has a stat with Pips > 0.
	swing.Update(&trader.CandleTime{
		Candle:    trader.Candle{Open: 108_000, High: 108_200, Low: 107_800, Close: 108_100},
		Timestamp: 1_000_000,
	})

	printAnalysis(&buf, inst, "EURUSD", "H1", from, to, []trader.Analyzer{swing}, 100_000, 1.08)
	out := buf.String()
	// Header should include the lot label.
	assert.Contains(t, out, "standard lot")
	// USD column for a pip stat should appear as "($...)".
	assert.Contains(t, out, "($")
}

// ── stats command validation ──────────────────────────────────────────────────

func TestNewStatsCmd_BlankInstrumentReturnsError(t *testing.T) {
	cmd := newStatsCmd(nil)
	_ = cmd.Flags().Set("instrument", "")
	_ = cmd.Flags().Set("from", "2024-01-01")
	_ = cmd.Flags().Set("to", "2024-12-31")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank --instrument")
}

func TestNewStatsCmd_UnknownInstrumentReturnsError(t *testing.T) {
	cmd := newStatsCmd(nil)
	_ = cmd.Flags().Set("instrument", "XXXXXX")
	_ = cmd.Flags().Set("from", "2024-01-01")
	_ = cmd.Flags().Set("to", "2024-12-31")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown instrument")
}

func TestNewStatsCmd_BadFromReturnsError(t *testing.T) {
	cmd := newStatsCmd(nil)
	_ = cmd.Flags().Set("instrument", "EURUSD")
	_ = cmd.Flags().Set("from", "not-a-date")
	_ = cmd.Flags().Set("to", "2024-12-31")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from")
}

func TestNewStatsCmd_BadToReturnsError(t *testing.T) {
	cmd := newStatsCmd(nil)
	_ = cmd.Flags().Set("instrument", "EURUSD")
	_ = cmd.Flags().Set("from", "2024-01-01")
	_ = cmd.Flags().Set("to", "bad-date")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--to")
}

func TestNewStatsCmd_FromNotBeforeToReturnsError(t *testing.T) {
	cmd := newStatsCmd(nil)
	_ = cmd.Flags().Set("instrument", "EURUSD")
	_ = cmd.Flags().Set("from", "2024-12-31")
	_ = cmd.Flags().Set("to", "2024-01-01")
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from must be before --to")
}
