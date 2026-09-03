package report_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/report"
)

func TestTextRenderer_Golden_Representative(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	assertGolden(t, "representative.txt", report.TextRenderer{}, bt)
}

func TestTextRenderer_Golden_ZeroTrade(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newZeroTradeResult(t)))
	assertGolden(t, "zero_trade.txt", report.TextRenderer{}, bt)
}

func TestTextRenderer_ShowsStrategyParametersWhenPresent(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	bt.Run.StrategyParameters = []byte(`{"fast_period":20,"slow_period":50}`)

	var buf bytes.Buffer
	require.NoError(t, report.TextRenderer{}.Render(&buf, bt))
	assert.Contains(t, buf.String(), `Strategy Params: {"fast_period":20,"slow_period":50}`)
}

func TestTextRenderer_OmitsNullStrategyParameters(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	bt.Run.StrategyParameters = []byte(`null`)

	var buf bytes.Buffer
	require.NoError(t, report.TextRenderer{}.Render(&buf, bt))
	assert.NotContains(t, buf.String(), "Strategy Params:")
}

func TestTextRenderer_ReportsEmptyDatasetExplicitly(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	bt.Dataset = nil

	var buf bytes.Buffer
	require.NoError(t, report.TextRenderer{}.Render(&buf, bt))
	assert.Contains(t, buf.String(), "Dataset:        (no dataset recorded)")
}
