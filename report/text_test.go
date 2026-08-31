package report_test

import (
	"testing"

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
