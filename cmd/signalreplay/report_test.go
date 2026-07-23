package signalreplay

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	backtestsvc "github.com/rustyeddy/trader/service/backtest"
)

// ── rMultiple / holdBars ──────────────────────────────────────────────────

func TestRMultiple_Long(t *testing.T) {
	t.Parallel()
	// Long: entry 1.10, stop 1.09 (risk 0.01), exit 1.12 (reward 0.02) -> R=2.
	assert.InDelta(t, 2.0, rMultiple("long", 1.10, 1.12, 1.09), 1e-9)
}

func TestRMultiple_Short(t *testing.T) {
	t.Parallel()
	// Short: entry 1.10, stop 1.11 (risk 0.01), exit 1.08 (reward 0.02) -> R=2.
	assert.InDelta(t, 2.0, rMultiple("short", 1.10, 1.08, 1.11), 1e-9)
}

func TestRMultiple_LosingTradeIsNegative(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, -1.0, rMultiple("long", 1.10, 1.09, 1.09), 1e-9)
}

func TestRMultiple_GuardsDivideByZero(t *testing.T) {
	t.Parallel()
	// initial_stop == entry (stop-at-entry edge case): must not divide by zero.
	assert.Equal(t, 0.0, rMultiple("long", 1.10, 1.12, 1.10))
}

func TestHoldBars_D1(t *testing.T) {
	t.Parallel()
	got := holdBars("2024-01-02T00:00:00Z", "2024-01-05T00:00:00Z", timeframeSeconds("D1"))
	assert.Equal(t, 3, got)
}

func TestHoldBars_UnrecognizedTimeframeReturnsZero(t *testing.T) {
	t.Parallel()
	got := holdBars("2024-01-02T00:00:00Z", "2024-01-05T00:00:00Z", timeframeSeconds("weekly"))
	assert.Equal(t, 0, got)
}

func TestHoldBars_UnparsableTimeReturnsZero(t *testing.T) {
	t.Parallel()
	got := holdBars("not-a-time", "2024-01-05T00:00:00Z", timeframeSeconds("D1"))
	assert.Equal(t, 0, got)
}

// ── buildOutcomeRow ────────────────────────────────────────────────────────

func TestBuildOutcomeRow_FiltersNonSignalreplayTrades(t *testing.T) {
	t.Parallel()
	tr := backtest.BacktestReportTrade{Reason: "donchian-v6-breakout-up"}
	_, ok := buildOutcomeRow(tr, nil, 86400)
	assert.False(t, ok)
}

func TestBuildOutcomeRow_FiltersUnparsableReasonDate(t *testing.T) {
	t.Parallel()
	tr := backtest.BacktestReportTrade{Reason: "signalreplay:max-hold"}
	_, ok := buildOutcomeRow(tr, nil, 86400)
	assert.False(t, ok, "close-only reasons like max-hold must not appear as entry Reason, but guard anyway")
}

func TestBuildOutcomeRow_JoinsFeatures(t *testing.T) {
	t.Parallel()
	features, err := loadSweepFeatures("testdata/report_fixture.csv")
	require.NoError(t, err)

	tr := backtest.BacktestReportTrade{
		Instrument:       "EUR_USD",
		Side:             "long",
		Reason:           "signalreplay:2024-01-02T00:00:00Z",
		OpenTime:         "2024-01-03T00:00:00Z",
		CloseTime:        "2024-01-08T00:00:00Z",
		OpenPrice:        1.10,
		ClosePrice:       1.12,
		InitialStopPrice: 1.09,
		CloseCause:       "StopLoss",
		PNL:              20.0,
	}
	row, ok := buildOutcomeRow(tr, features, timeframeSeconds("D1"))
	require.True(t, ok)
	assert.Equal(t, "2024-01-02T00:00:00Z", row.SignalDate)
	assert.Equal(t, "EURUSD", row.Instrument)
	assert.Equal(t, "long", row.Bias)
	assert.Equal(t, 5, row.HoldBars)
	assert.InDelta(t, 2.0, row.RMultiple, 1e-9)
	require.NotNil(t, row.Features)
	assert.Equal(t, "28.4", row.Features["ADX"])
	assert.Equal(t, "✓", row.Features["W1 Bias"])
}

func TestBuildOutcomeRow_NoFeatureMatchLeavesFeaturesNil(t *testing.T) {
	t.Parallel()
	features, err := loadSweepFeatures("testdata/report_fixture.csv")
	require.NoError(t, err)

	tr := backtest.BacktestReportTrade{
		Instrument: "AUDNZD",
		Reason:     "signalreplay:2099-01-01T00:00:00Z",
		OpenTime:   "2099-01-02T00:00:00Z",
		CloseTime:  "2099-01-03T00:00:00Z",
	}
	row, ok := buildOutcomeRow(tr, features, timeframeSeconds("D1"))
	require.True(t, ok)
	assert.Nil(t, row.Features)
}

// ── loadSweepFeatures ──────────────────────────────────────────────────────

func TestLoadSweepFeatures_MissingColumn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv")
	require.NoError(t, os.WriteFile(path, []byte("DATE,PAIR\n2024-01-02T00:00:00Z,EURUSD\n"), 0o644))
	_, err := loadSweepFeatures(path)
	assert.ErrorContains(t, err, "ADX")
}

func TestLoadSweepFeatures_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := loadSweepFeatures("testdata/does-not-exist.csv")
	assert.Error(t, err)
}

// ── BuildOutcomeRows (integration over real JSON report files) ────────────

func writeSampleReport(t *testing.T, dir, name string, trades []backtest.BacktestReportTrade) {
	t.Helper()
	summary := backtest.BacktestReportSummary{
		Name:         name,
		Instrument:   "EURUSD",
		Timeframe:    "d1",
		TradeDetails: trades,
	}
	require.NoError(t, backtestsvc.WriteBacktestSummaryJSON(filepath.Join(dir, name+".json"), summary))
}

func TestBuildOutcomeRows_EndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSampleReport(t, dir, "eurusd-signalreplay", []backtest.BacktestReportTrade{
		{
			Instrument: "EUR_USD", Side: "long", Reason: "signalreplay:2024-01-02T00:00:00Z",
			OpenTime: "2024-01-03T00:00:00Z", CloseTime: "2024-01-08T00:00:00Z",
			OpenPrice: 1.10, ClosePrice: 1.12, InitialStopPrice: 1.09, CloseCause: "StopLoss", PNL: 20,
		},
		{
			// Not a signalreplay trade (different strategy's reason) — must be filtered out.
			Instrument: "EUR_USD", Side: "short", Reason: "donchian-v6-breakout-down",
			OpenTime: "2024-02-01T00:00:00Z", CloseTime: "2024-02-05T00:00:00Z",
		},
	})
	writeSampleReport(t, dir, "gbpusd-signalreplay", []backtest.BacktestReportTrade{
		{
			Instrument: "GBP_USD", Side: "short", Reason: "signalreplay:2024-01-15T00:00:00Z",
			OpenTime: "2024-01-16T00:00:00Z", CloseTime: "2024-01-20T00:00:00Z",
			OpenPrice: 1.27, ClosePrice: 1.25, InitialStopPrice: 1.28, CloseCause: "TakeProfit", PNL: 40,
		},
	})

	rows, err := BuildOutcomeRows(dir, "testdata/report_fixture.csv")
	require.NoError(t, err)
	require.Len(t, rows, 2, "the non-signalreplay trade must be filtered")

	// Deterministic order: sorted by (instrument, entry_time).
	assert.Equal(t, "EURUSD", rows[0].Instrument)
	assert.Equal(t, "GBPUSD", rows[1].Instrument)
}

func TestBuildOutcomeRows_ErrorsWhenNoReportsFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := BuildOutcomeRows(dir, "testdata/report_fixture.csv")
	assert.ErrorContains(t, err, "no backtest reports found")
}

func TestBuildOutcomeRows_PropagatesSignalsLoadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSampleReport(t, dir, "r1", []backtest.BacktestReportTrade{{Reason: "signalreplay:2024-01-02T00:00:00Z"}})
	_, err := BuildOutcomeRows(dir, "testdata/does-not-exist.csv")
	assert.Error(t, err)
}

// ── WriteOutcomeCSV / determinism ──────────────────────────────────────────

func TestWriteOutcomeCSV_Golden(t *testing.T) {
	t.Parallel()
	rows := []OutcomeRow{
		{
			SignalDate: "2024-01-02T00:00:00Z", Instrument: "EURUSD", Bias: "long",
			EntryTime: "2024-01-03T00:00:00Z", EntryPrice: 1.10, InitialStop: 1.09,
			ExitTime: "2024-01-08T00:00:00Z", ExitPrice: 1.12, CloseCause: "StopLoss",
			PNL: 20, RMultiple: 2, HoldBars: 5,
			Features: map[string]string{"ADX": "28.4", "W1 Bias": "✓"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteOutcomeCSV(&buf, rows))

	want := "signal_date,instrument,bias,entry_time,entry_price,initial_stop,exit_time,exit_price,close_cause,pnl,r_multiple,hold_bars,ADX,CI,EMA SEP,EMA DIST,H4 ADX,H4 CI,H4 EMA DIST,Squeeze,W1 Bias,WEEK%,H1 Align,H1 EMA DIST\n" +
		"2024-01-02T00:00:00Z,EURUSD,long,2024-01-03T00:00:00Z,1.1,1.09,2024-01-08T00:00:00Z,1.12,StopLoss,20,2.0000,5,28.4,,,,,,,,✓,,,\n"
	assert.Equal(t, want, buf.String())
}

func TestBuildOutcomeRows_DeterministicAcrossRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSampleReport(t, dir, "eurusd-signalreplay", []backtest.BacktestReportTrade{
		{
			Instrument: "EUR_USD", Side: "long", Reason: "signalreplay:2024-01-02T00:00:00Z",
			OpenTime: "2024-01-03T00:00:00Z", CloseTime: "2024-01-08T00:00:00Z",
			OpenPrice: 1.10, ClosePrice: 1.12, InitialStopPrice: 1.09, CloseCause: "StopLoss", PNL: 20,
		},
	})

	rows1, err := BuildOutcomeRows(dir, "testdata/report_fixture.csv")
	require.NoError(t, err)
	rows2, err := BuildOutcomeRows(dir, "testdata/report_fixture.csv")
	require.NoError(t, err)

	var b1, b2 bytes.Buffer
	require.NoError(t, WriteOutcomeCSV(&b1, rows1))
	require.NoError(t, WriteOutcomeCSV(&b2, rows2))
	assert.Equal(t, b1.String(), b2.String())
}

// ── Summarize / PrintOutcomeSummary ────────────────────────────────────────

func TestSummarize_ComputesAggregates(t *testing.T) {
	t.Parallel()
	rows := []OutcomeRow{
		{Instrument: "EURUSD", CloseCause: "StopLoss", PNL: -10, RMultiple: -1},
		{Instrument: "EURUSD", CloseCause: "TakeProfit", PNL: 20, RMultiple: 2},
		{Instrument: "GBPUSD", CloseCause: "StopLoss", PNL: -5, RMultiple: -0.5},
	}
	s := Summarize(rows)
	assert.Equal(t, 3, s.TradeCount)
	assert.InDelta(t, 33.333, s.WinRate, 0.01)
	assert.InDelta(t, 0.1667, s.AvgR, 0.001)
	assert.InDelta(t, 1.6667, s.Expectancy, 0.001)

	require.Contains(t, s.ByPair, "EURUSD")
	assert.Equal(t, 2, s.ByPair["EURUSD"].Count)
	assert.InDelta(t, 0.5, s.ByPair["EURUSD"].AvgR, 1e-9)

	require.Contains(t, s.ByCloseCause, "StopLoss")
	assert.Equal(t, 2, s.ByCloseCause["StopLoss"].Count)
}

func TestSummarize_EmptyRows(t *testing.T) {
	t.Parallel()
	s := Summarize(nil)
	assert.Equal(t, 0, s.TradeCount)
	assert.Equal(t, 0.0, s.WinRate)
	assert.Empty(t, s.ByPair)
}

func TestPrintOutcomeSummary_Deterministic(t *testing.T) {
	t.Parallel()
	rows := []OutcomeRow{
		{Instrument: "USDJPY", CloseCause: "Manual", PNL: 5, RMultiple: 0.5},
		{Instrument: "AUDUSD", CloseCause: "StopLoss", PNL: -5, RMultiple: -0.5},
	}
	s := Summarize(rows)
	var buf bytes.Buffer
	PrintOutcomeSummary(&buf, s)
	out := buf.String()
	// Alphabetical: AUDUSD before USDJPY, Manual before StopLoss.
	assert.Less(t, indexOf(out, "AUDUSD"), indexOf(out, "USDJPY"))
	assert.Less(t, indexOf(out, "Manual"), indexOf(out, "StopLoss"))
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
