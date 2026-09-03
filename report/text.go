package report

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// TextRenderer renders a compact, terminal-friendly summary of a
// BacktestReport: run identity, performance, trade statistics, and a
// per-instrument breakdown. It deliberately omits the closed/open
// trade lists and the full equity curve (issue #220 review, point 8:
// a run can have thousands of trades or equity points, and this
// renderer targets a terminal glance-review, not an audit artifact) —
// use JSONRenderer or OrgRenderer for the complete series.
type TextRenderer struct{}

// Render implements Renderer[BacktestReport].
func (TextRenderer) Render(w io.Writer, report BacktestReport) error {
	ew := &errWriter{w: w}

	run := report.Run
	_, _ = fmt.Fprintf(ew, "Backtest Report: %s", run.StrategyName)
	if run.StrategyVersion != "" {
		_, _ = fmt.Fprintf(ew, " %s", run.StrategyVersion)
	}
	_, _ = fmt.Fprintln(ew)
	_, _ = fmt.Fprintf(ew, "Run ID:         %s\n", run.RunID)
	_, _ = fmt.Fprintf(ew, "Span:           %s to %s\n", formatTime(run.SpanStart), formatTime(run.SpanEnd))
	_, _ = fmt.Fprintf(ew, "Config Digest:  %s\n", run.ConfigDigest)
	for _, d := range report.Dataset {
		_, _ = fmt.Fprintf(ew, "Dataset:        %s %s %s rev=%s\n", d.Provider, d.Instrument, d.Interval, d.Revision)
	}
	_, _ = fmt.Fprintln(ew)

	perf := report.Performance
	_, _ = fmt.Fprintln(ew, "Performance")
	tw := tabwriter.NewWriter(ew, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "  Starting Capital:\t%s\n", formatMoney(perf.StartingCapital))
	_, _ = fmt.Fprintf(tw, "  Final Equity:\t%s\n", formatMoney(perf.FinalEquity))
	_, _ = fmt.Fprintf(tw, "  Net Return:\t%s\n", formatRate(perf.NetReturn))
	_, _ = fmt.Fprintf(tw, "  Max Drawdown:\t%s\n", formatRate(perf.MaxDrawdown))
	_ = tw.Flush()
	_, _ = fmt.Fprintln(ew)

	ts := report.TradeStats
	_, _ = fmt.Fprintln(ew, "Trade Statistics")
	tw = tabwriter.NewWriter(ew, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "  Trades:\t%d\n", ts.TradeCount)
	_, _ = fmt.Fprintf(tw, "  Wins / Losses:\t%d / %d\n", ts.Wins, ts.Losses)
	_, _ = fmt.Fprintf(tw, "  Win Rate:\t%s\n", formatOptionalRate(ts.WinRate))
	_, _ = fmt.Fprintf(tw, "  Average Win:\t%s\n", formatOptionalMoney(ts.AverageWin))
	_, _ = fmt.Fprintf(tw, "  Average Loss:\t%s\n", formatOptionalMoney(ts.AverageLoss))
	_, _ = fmt.Fprintf(tw, "  Gross PnL:\t%s\n", formatMoney(ts.GrossPnL))
	_, _ = fmt.Fprintf(tw, "  Closed Trade Costs:\t%s\n", formatMoney(ts.ClosedTradeCosts))
	_, _ = fmt.Fprintf(tw, "  Account Fees:\t%s\n", formatMoney(ts.AccountFees))
	_, _ = fmt.Fprintf(tw, "  Net PnL:\t%s\n", formatMoney(ts.NetPnL))
	_, _ = fmt.Fprintf(tw, "  Expectancy:\t%s\n", formatOptionalMoney(ts.Expectancy))
	_, _ = fmt.Fprintf(tw, "  Profit Factor:\t%s\n", formatOptionalRate(ts.ProfitFactor))
	_ = tw.Flush()
	_, _ = fmt.Fprintln(ew)

	_, _ = fmt.Fprintln(ew, "Per-Instrument Results")
	if len(report.PerInstrument) == 0 {
		_, _ = fmt.Fprintln(ew, "  (no closed trades)")
	} else {
		tw = tabwriter.NewWriter(ew, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  Instrument\tProvider\tVenue\tCount\tWins\tLosses\tGross PnL\tCosts\tNet PnL")
		for _, im := range report.PerInstrument {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\n",
				im.Instrument, im.Provider, im.Venue, im.Count, im.Wins, im.Losses,
				formatMoney(im.GrossPnL), formatMoney(im.Costs), formatMoney(im.NetPnL))
		}
		_ = tw.Flush()
	}
	_, _ = fmt.Fprintln(ew)

	_, _ = fmt.Fprintln(ew, "Long/Short Breakdown")
	if len(report.BySide) == 0 {
		_, _ = fmt.Fprintln(ew, "  (no closed trades)")
	} else {
		tw = tabwriter.NewWriter(ew, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  Side\tCount\tWins\tLosses\tGross PnL\tCosts\tNet PnL")
		for _, s := range report.BySide {
			_, _ = fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%s\t%s\t%s\n",
				s.Side, s.Count, s.Wins, s.Losses, formatMoney(s.GrossPnL), formatMoney(s.Costs), formatMoney(s.NetPnL))
		}
		_ = tw.Flush()
	}
	_, _ = fmt.Fprintln(ew)

	acct := report.Account
	_, _ = fmt.Fprintln(ew, "Final Account State")
	tw = tabwriter.NewWriter(ew, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "  Equity:\t%s\n", formatMoney(acct.Equity))
	_, _ = fmt.Fprintf(tw, "  Realized PnL:\t%s\n", formatMoney(acct.RealizedPnL))
	_, _ = fmt.Fprintf(tw, "  Unrealized PnL:\t%s\n", formatMoney(acct.UnrealizedPnL))
	_, _ = fmt.Fprintf(tw, "  Open Positions:\t%d\n", len(acct.OpenPositions))
	_, _ = fmt.Fprintf(tw, "  Open Orders:\t%d\n", acct.OpenOrderCount)
	_ = tw.Flush()
	_, _ = fmt.Fprintln(ew)

	_, _ = fmt.Fprintln(ew, "(full trade list and equity curve omitted — see JSON or Org export)")

	return ew.err
}
