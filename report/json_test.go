package report_test

import (
	"testing"

	"github.com/rustyeddy/trader/report"
)

func TestJSONRenderer_Golden_Representative(t *testing.T) {
	bt := report.NewBacktestReport(newRepresentativeResult(t))
	assertGolden(t, "representative.json", report.JSONRenderer{}, bt)
}

func TestJSONRenderer_Golden_ZeroTrade(t *testing.T) {
	bt := report.NewBacktestReport(newZeroTradeResult(t))
	assertGolden(t, "zero_trade.json", report.JSONRenderer{}, bt)
}
