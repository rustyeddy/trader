package backtest

import "github.com/rustyeddy/trader/internal/report"

// NewReport projects a run's response into the transport-neutral report
// model (report.BacktestReport). It is the one report-assembly step every
// transport shares (ADR-069 Decision 3); RunResponse itself still carries
// no reporting concerns (ADR-039), and rendering stays in each transport.
func NewReport(resp RunResponse) report.BacktestReport {
	return report.NewBacktestReport(report.BacktestInput{
		Manifest:         resp.Manifest,
		Account:          resp.Account,
		Trades:           resp.Trades,
		OpenTrades:       resp.OpenTrades,
		EquityCurve:      resp.EquityCurve,
		Metrics:          resp.Metrics,
		MarginRejections: resp.MarginRejections,
	})
}
