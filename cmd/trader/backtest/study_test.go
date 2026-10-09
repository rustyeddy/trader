package backtest

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/journal"
	"github.com/rustyeddy/trader/internal/report"
)

type recordingRecorder struct {
	kinds  []journal.Kind
	closed bool
}

func (r *recordingRecorder) Record(_ context.Context, rec journal.Record) error {
	r.kinds = append(r.kinds, rec.Kind)
	return nil
}
func (r *recordingRecorder) Close() error { r.closed = true; return nil }

func TestSignalCounter_CountsSignalsAndForwardsEverything(t *testing.T) {
	inner := &recordingRecorder{}
	c := &signalCounter{inner: inner}
	ctx := context.Background()
	for _, k := range []journal.Kind{journal.KindSignal, journal.KindIntent, journal.KindSignal} {
		require.NoError(t, c.Record(ctx, journal.Record{Kind: k}))
	}
	assert.Equal(t, 2, c.signals)
	assert.Len(t, inner.kinds, 3, "every record is forwarded, not only signals")
	require.NoError(t, c.Close())
	assert.True(t, inner.closed)
}

func TestSignalCounter_WorksWithoutAJournal(t *testing.T) {
	c := &signalCounter{}
	require.NoError(t, c.Record(context.Background(), journal.Record{Kind: journal.KindSignal}))
	require.NoError(t, c.Close())
	assert.Equal(t, 1, c.signals)
}

func studyReport() report.BacktestReport {
	return report.BacktestReport{
		Run: report.RunInfo{
			RunID: "run_1", StrategyName: "forex-atr", StrategyVersion: "0.3.0",
			SpanStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), SpanEnd: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		Dataset: []report.DatasetReport{
			{Instrument: "fx:EUR/USD", Interval: "D1"},
			{Instrument: "fx:GBP/USD", Interval: "D1"},
			{Instrument: "fx:EUR/USD", Interval: "D1"},
		},
	}
}

func TestNewStudySummary(t *testing.T) {
	sum := newStudySummary(studyReport(), 42, "signals.jsonl")
	assert.Equal(t, "forex-atr", sum.Strategy)
	assert.Equal(t, 2, sum.Instruments, "distinct instruments, not datasets")
	assert.Equal(t, "D1", sum.Interval)
	assert.Equal(t, 42, sum.Signals)

	rep := studyReport()
	rep.Dataset = append(rep.Dataset, report.DatasetReport{Instrument: "fx:EUR/USD", Interval: "H1"})
	assert.Equal(t, "[D1 H1]", newStudySummary(rep, 0, "").Interval)
}

func TestRenderStudy(t *testing.T) {
	t.Run("table says nothing about trades and shows the journal", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, renderStudy(&out, formatTable, newStudySummary(studyReport(), 42, "signals.jsonl")))
		got := out.String()
		assert.Contains(t, got, "Study: forex-atr 0.3.0")
		assert.Contains(t, got, "2020-01-01 to 2024-12-31")
		assert.Contains(t, got, "Instruments:  2")
		assert.Contains(t, got, "42 (journaled to signals.jsonl)")
		for _, backtestOnly := range []string{"Trades", "Equity", "PnL", "Drawdown", "Win Rate"} {
			assert.NotContains(t, got, backtestOnly)
		}
	})
	t.Run("without a journal it says how to keep the signals", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, renderStudy(&out, formatTable, newStudySummary(studyReport(), 7, "")))
		assert.Contains(t, out.String(), "pass --journal PATH to keep them")
	})
	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, renderStudy(&out, formatJSON, newStudySummary(studyReport(), 42, "")))
		var got studySummary
		require.NoError(t, json.Unmarshal(out.Bytes(), &got))
		assert.Equal(t, 42, got.Signals)
		assert.Equal(t, "run_1", got.RunID)
		assert.NotContains(t, out.String(), "closed_trades")
	})
	t.Run("org renders as text", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, renderStudy(&out, formatOrg, newStudySummary(studyReport(), 1, "")))
		assert.Contains(t, out.String(), "Study:")
	})
	t.Run("unknown format", func(t *testing.T) {
		require.ErrorContains(t, renderStudy(&bytes.Buffer{}, "yaml", studySummary{}), "invalid --format")
	})
}
