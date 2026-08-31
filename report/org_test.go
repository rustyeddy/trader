package report_test

import (
	"testing"

	"github.com/rustyeddy/trader/report"
)

func TestOrgRenderer_Golden_Representative(t *testing.T) {
	bt := report.NewBacktestReport(newRepresentativeResult(t))
	assertGolden(t, "representative.org", report.OrgRenderer{}, bt)
}

func TestOrgRenderer_Golden_ZeroTrade(t *testing.T) {
	bt := report.NewBacktestReport(newZeroTradeResult(t))
	assertGolden(t, "zero_trade.org", report.OrgRenderer{}, bt)
}
