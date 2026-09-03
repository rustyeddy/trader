package report_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/report"
)

func TestOrgRenderer_Golden_Representative(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	assertGolden(t, "representative.org", report.OrgRenderer{}, bt)
}

func TestOrgRenderer_Golden_ZeroTrade(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newZeroTradeResult(t)))
	assertGolden(t, "zero_trade.org", report.OrgRenderer{}, bt)
}

func TestOrgRenderer_ShowsStrategyParametersWhenPresent(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	bt.Run.StrategyParameters = []byte(`{"fast_period":20,"slow_period":50}`)

	var buf bytes.Buffer
	require.NoError(t, report.OrgRenderer{}.Render(&buf, bt))
	assert.Contains(t, buf.String(), `:STRATEGY_PARAMETERS: {"fast_period":20,"slow_period":50}`)
}

func TestOrgRenderer_ReportsEmptyDatasetExplicitly(t *testing.T) {
	bt := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t)))
	bt.Dataset = nil

	var buf bytes.Buffer
	require.NoError(t, report.OrgRenderer{}.Render(&buf, bt))
	assert.Contains(t, buf.String(), "(no dataset recorded)")
}
