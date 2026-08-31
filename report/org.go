package report

import (
	"fmt"
	"io"
)

// OrgRenderer renders a BacktestReport as an Org-mode document — the
// first-class rendering per the architecture doc's "Org mode should be
// a first-class renderer" guidance. Unlike TextRenderer, it includes
// the complete closed/open trade lists and the full equity curve, so
// the resulting .org file remains a complete, auditable research
// artifact against the run's manifest and journal (issue #220 review,
// point 4).
type OrgRenderer struct{}

// Render implements Renderer[BacktestReport].
func (OrgRenderer) Render(w io.Writer, report BacktestReport) error {
	ew := &errWriter{w: w}

	run := report.Run
	_, _ = fmt.Fprintf(ew, "#+TITLE: Backtest Report: %s", run.StrategyName)
	if run.StrategyVersion != "" {
		_, _ = fmt.Fprintf(ew, " %s", run.StrategyVersion)
	}
	_, _ = fmt.Fprintln(ew)
	_, _ = fmt.Fprintln(ew, ":PROPERTIES:")
	_, _ = fmt.Fprintf(ew, ":RUN_ID: %s\n", run.RunID)
	_, _ = fmt.Fprintf(ew, ":CONFIG_DIGEST: %s\n", run.ConfigDigest)
	_, _ = fmt.Fprintf(ew, ":SPAN_START: %s\n", formatTime(run.SpanStart))
	_, _ = fmt.Fprintf(ew, ":SPAN_END: %s\n", formatTime(run.SpanEnd))
	if run.TraderVersion != "" {
		_, _ = fmt.Fprintf(ew, ":TRADER_VERSION: %s\n", run.TraderVersion)
	}
	_, _ = fmt.Fprintln(ew, ":END:")
	_, _ = fmt.Fprintln(ew)

	orgPerformanceSection(ew, report.Performance)
	orgTradeStatsSection(ew, report.TradeStats)
	orgPerInstrumentSection(ew, report.PerInstrument)
	orgTradesSection(ew, "Completed Trades", report.ClosedTrades)
	orgTradesSection(ew, "Open Trades", report.OpenTrades)
	orgAccountSection(ew, report.Account)
	orgEquityCurveSection(ew, report.EquityCurve)

	return ew.err
}

func orgPerformanceSection(ew *errWriter, perf Performance) {
	_, _ = fmt.Fprintln(ew, "* Performance Summary")
	_, _ = fmt.Fprintln(ew, "| Metric | Value |")
	_, _ = fmt.Fprintln(ew, "|--------+-------|")
	_, _ = fmt.Fprintf(ew, "| Starting Capital | %s |\n", formatMoney(perf.StartingCapital))
	_, _ = fmt.Fprintf(ew, "| Final Equity | %s |\n", formatMoney(perf.FinalEquity))
	_, _ = fmt.Fprintf(ew, "| Net Return | %s |\n", formatRate(perf.NetReturn))
	_, _ = fmt.Fprintf(ew, "| Max Drawdown | %s |\n", formatRate(perf.MaxDrawdown))
	_, _ = fmt.Fprintln(ew)
}

func orgTradeStatsSection(ew *errWriter, ts TradeStats) {
	_, _ = fmt.Fprintln(ew, "* Trade Statistics")
	_, _ = fmt.Fprintln(ew, "| Metric | Value |")
	_, _ = fmt.Fprintln(ew, "|--------+-------|")
	_, _ = fmt.Fprintf(ew, "| Trade Count | %d |\n", ts.TradeCount)
	_, _ = fmt.Fprintf(ew, "| Wins | %d |\n", ts.Wins)
	_, _ = fmt.Fprintf(ew, "| Losses | %d |\n", ts.Losses)
	_, _ = fmt.Fprintf(ew, "| Win Rate | %s |\n", formatOptionalRate(ts.WinRate))
	_, _ = fmt.Fprintf(ew, "| Average Win | %s |\n", formatOptionalMoney(ts.AverageWin))
	_, _ = fmt.Fprintf(ew, "| Average Loss | %s |\n", formatOptionalMoney(ts.AverageLoss))
	_, _ = fmt.Fprintf(ew, "| Gross PnL | %s |\n", formatMoney(ts.GrossPnL))
	_, _ = fmt.Fprintf(ew, "| Closed Trade Costs | %s |\n", formatMoney(ts.ClosedTradeCosts))
	_, _ = fmt.Fprintf(ew, "| Account Fees | %s |\n", formatMoney(ts.AccountFees))
	_, _ = fmt.Fprintf(ew, "| Net PnL | %s |\n", formatMoney(ts.NetPnL))
	_, _ = fmt.Fprintf(ew, "| Expectancy | %s |\n", formatOptionalMoney(ts.Expectancy))
	_, _ = fmt.Fprintf(ew, "| Profit Factor | %s |\n", formatOptionalRate(ts.ProfitFactor))
	_, _ = fmt.Fprintln(ew)
}

func orgPerInstrumentSection(ew *errWriter, rows []InstrumentReport) {
	_, _ = fmt.Fprintln(ew, "* Per-Instrument Results")
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(ew, "(no closed trades)")
		_, _ = fmt.Fprintln(ew)
		return
	}
	_, _ = fmt.Fprintln(ew, "| Instrument | Provider | Venue | Count | Wins | Losses | Gross PnL | Costs | Net PnL |")
	_, _ = fmt.Fprintln(ew, "|------------+----------+-------+-------+------+--------+-----------+-------+---------|")
	for _, im := range rows {
		_, _ = fmt.Fprintf(ew, "| %s | %s | %s | %d | %d | %d | %s | %s | %s |\n",
			im.Instrument, im.Provider, im.Venue, im.Count, im.Wins, im.Losses,
			formatMoney(im.GrossPnL), formatMoney(im.Costs), formatMoney(im.NetPnL))
	}
	_, _ = fmt.Fprintln(ew)
}

func orgTradesSection(ew *errWriter, title string, trades []TradeReport) {
	_, _ = fmt.Fprintf(ew, "* %s\n", title)
	if len(trades) == 0 {
		_, _ = fmt.Fprintln(ew, "(none)")
		_, _ = fmt.Fprintln(ew)
		return
	}
	_, _ = fmt.Fprintln(ew, "| Instrument | Provider | Venue | Side | Opened At | Closed At | Realized PnL | Costs |")
	_, _ = fmt.Fprintln(ew, "|------------+----------+-------+------+-----------+-----------+--------------+-------|")
	for _, t := range trades {
		_, _ = fmt.Fprintf(ew, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			t.Instrument, t.Provider, t.Venue, t.Side,
			formatTime(t.OpenedAt), formatTime(t.ClosedAt),
			formatMoney(t.RealizedPnL), formatMoney(t.Costs))
	}
	_, _ = fmt.Fprintln(ew)
}

func orgAccountSection(ew *errWriter, acct AccountReport) {
	_, _ = fmt.Fprintln(ew, "* Final Account State")
	_, _ = fmt.Fprintln(ew, "| Metric | Value |")
	_, _ = fmt.Fprintln(ew, "|--------+-------|")
	_, _ = fmt.Fprintf(ew, "| Account ID | %s |\n", acct.AccountID)
	_, _ = fmt.Fprintf(ew, "| Broker | %s |\n", acct.Broker)
	_, _ = fmt.Fprintf(ew, "| Currency | %s |\n", acct.Currency)
	_, _ = fmt.Fprintf(ew, "| As Of | %s |\n", formatTime(acct.AsOf))
	_, _ = fmt.Fprintf(ew, "| Equity | %s |\n", formatMoney(acct.Equity))
	_, _ = fmt.Fprintf(ew, "| Buying Power | %s |\n", formatMoney(acct.BuyingPower))
	_, _ = fmt.Fprintf(ew, "| Margin Used | %s |\n", formatMoney(acct.MarginUsed))
	_, _ = fmt.Fprintf(ew, "| Margin Available | %s |\n", formatMoney(acct.MarginAvailable))
	_, _ = fmt.Fprintf(ew, "| Realized PnL | %s |\n", formatMoney(acct.RealizedPnL))
	_, _ = fmt.Fprintf(ew, "| Unrealized PnL | %s |\n", formatMoney(acct.UnrealizedPnL))
	_, _ = fmt.Fprintf(ew, "| Fees | %s |\n", formatMoney(acct.Fees))
	_, _ = fmt.Fprintf(ew, "| Financing | %s |\n", formatMoney(acct.Financing))
	_, _ = fmt.Fprintf(ew, "| Open Order Count | %d |\n", acct.OpenOrderCount)
	_, _ = fmt.Fprintln(ew)

	_, _ = fmt.Fprintln(ew, "** Open Positions")
	if len(acct.OpenPositions) == 0 {
		_, _ = fmt.Fprintln(ew, "(none)")
		_, _ = fmt.Fprintln(ew)
		return
	}
	_, _ = fmt.Fprintln(ew, "| Instrument | Provider | Venue | Side | Quantity | Avg Price |")
	_, _ = fmt.Fprintln(ew, "|------------+----------+-------+------+----------+-----------|")
	for _, p := range acct.OpenPositions {
		avg := undefinedValue
		if p.AvgPrice != nil {
			avg = p.AvgPrice.String()
		}
		_, _ = fmt.Fprintf(ew, "| %s | %s | %s | %s | %s | %s |\n",
			p.Instrument, p.Provider, p.Venue, p.Side, p.Quantity.String(), avg)
	}
	_, _ = fmt.Fprintln(ew)
}

func orgEquityCurveSection(ew *errWriter, curve []EquityPointReport) {
	_, _ = fmt.Fprintln(ew, "* Equity Curve")
	if len(curve) == 0 {
		_, _ = fmt.Fprintln(ew, "(no equity observations)")
		return
	}
	_, _ = fmt.Fprintln(ew, "| Timestamp | Equity |")
	_, _ = fmt.Fprintln(ew, "|-----------+--------|")
	for _, p := range curve {
		_, _ = fmt.Fprintf(ew, "| %s | %s |\n", formatTime(p.Timestamp), formatMoney(p.Equity))
	}
}
