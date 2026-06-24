package backtest

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// WriteOrgReport writes a full per-run org-mode report to w.
func WriteOrgReport(w io.Writer, s BacktestReportSummary) {
	start := shortDate(s.Start)
	end := shortDate(s.End)
	title := fmt.Sprintf("%s — %s %s  %s→%s",
		s.Strategy, s.Instrument, strings.ToUpper(s.Timeframe), start, end)

	fmt.Fprintf(w, "#+TITLE: Backtest: %s\n", title)
	fmt.Fprintf(w, "#+DATE: %s\n\n", time.Now().UTC().Format("2006-01-02"))

	// Top-level heading with machine-readable properties.
	fmt.Fprintf(w, "* %s\n", title)
	writeProperties(w, s)

	// Human-readable summary table.
	fmt.Fprintln(w, "\n** Summary")
	writeSummaryTable(w, s)

	// Monthly breakdown.
	if len(s.TradeDetails) > 0 {
		fmt.Fprintln(w, "\n** Monthly Breakdown")
		writeMonthlyTable(w, s)

		fmt.Fprintln(w, "\n** Trades")
		writeTradeTable(w, s.TradeDetails)
	}
}

// WriteOrgIndex writes a single comparison table across all summaries to w.
func WriteOrgIndex(w io.Writer, summaries []BacktestReportSummary) {
	fmt.Fprintf(w, "#+TITLE: Backtest Index\n")
	fmt.Fprintf(w, "#+DATE: %s\n\n", time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintln(w, "* Results")

	tbl := newOrgTable("Run", "Strategy", "Pair", "TF", "Period", "Trades", "Win%", "Net P/L", "Return", "Drawdown", "RR", "Stop")
	tbl.setRight(5, 6, 7, 8, 9, 10)

	for _, s := range summaries {
		start := shortDate(s.Start)
		end := shortDate(s.End)
		period := start + "→" + end

		dd := "—"
		if s.MaxDrawdown < 0 {
			dd = fmt.Sprintf("-$%.0f", -s.MaxDrawdown)
		}
		rr := "—"
		if s.RR > 0 {
			rr = fmt.Sprintf("%.2f", s.RR)
		}
		netPL := fmt.Sprintf("%+.2f", s.NetPL)
		ret := fmt.Sprintf("%+.2f%%", s.ReturnPct)

		tbl.addRow(
			s.Name,
			s.Strategy,
			s.Instrument,
			strings.ToUpper(s.Timeframe),
			period,
			fmt.Sprintf("%d", s.Trades),
			fmt.Sprintf("%.1f%%", s.WinRate),
			netPL,
			ret,
			dd,
			rr,
			s.Stop,
		)
	}
	tbl.write(w, "  ")
}

// ── internal helpers ──────────────────────────────────────────────────────────

func writeProperties(w io.Writer, s BacktestReportSummary) {
	fmt.Fprintln(w, "  :PROPERTIES:")
	prop := func(k, v string) { fmt.Fprintf(w, "  %-16s %s\n", ":"+k+":", v) }

	prop("strategy", s.Strategy)
	prop("instrument", s.Instrument)
	prop("timeframe", strings.ToUpper(s.Timeframe))
	prop("start", shortDate(s.Start))
	prop("end", shortDate(s.End))
	prop("trades", fmt.Sprintf("%d", s.Trades))
	prop("wins", fmt.Sprintf("%d", s.Wins))
	prop("losses", fmt.Sprintf("%d", s.Losses))
	prop("win_rate", fmt.Sprintf("%.1f%%", s.WinRate))
	prop("net_pl", fmt.Sprintf("%.2f", s.NetPL))
	prop("return_pct", fmt.Sprintf("%.2f%%", s.ReturnPct))
	prop("max_drawdown", fmt.Sprintf("%.2f", s.MaxDrawdown))
	prop("avg_winner", fmt.Sprintf("%.2f", s.AvgWinner))
	prop("avg_loser", fmt.Sprintf("%.2f", s.AvgLoser))
	prop("rr", fmt.Sprintf("%.2f", s.RR))
	prop("risk_pct", fmt.Sprintf("%.2f%%", s.RiskPct))
	prop("stop", s.Stop)
	if s.Regime != "" {
		prop("regime", s.Regime)
	}
	fmt.Fprintln(w, "  :END:")
}

func writeSummaryTable(w io.Writer, s BacktestReportSummary) {
	tbl := newOrgTable("Metric", "Value")

	start := shortDate(s.Start)
	end := shortDate(s.End)

	ddStr := "—"
	if s.MaxDrawdown < 0 {
		ddStr = fmt.Sprintf("-$%.2f", -s.MaxDrawdown)
	}
	rrStr := "—"
	if s.RR > 0 {
		rrStr = fmt.Sprintf("%.2f", s.RR)
	}
	stopStr := s.Stop
	if stopStr == "" {
		stopStr = "—"
	}

	tbl.addRow("Strategy", s.Strategy)
	tbl.addRow("Instrument", fmt.Sprintf("%s %s", s.Instrument, strings.ToUpper(s.Timeframe)))
	tbl.addRow("Period", fmt.Sprintf("%s → %s", start, end))
	tbl.addRow("Trades", fmt.Sprintf("%d", s.Trades))
	tbl.addRow("Wins", fmt.Sprintf("%d  (%.1f%%)", s.Wins, s.WinRate))
	tbl.addRow("Losses", fmt.Sprintf("%d  (%.1f%%)", s.Losses, 100-s.WinRate))
	tbl.addRow("Start Balance", fmt.Sprintf("$%.2f", s.StartBalance))
	tbl.addRow("End Balance", fmt.Sprintf("$%.2f", s.EndBalance))
	tbl.addRow("Net P/L", fmt.Sprintf("%+.2f", s.NetPL))
	tbl.addRow("Return", fmt.Sprintf("%+.2f%%", s.ReturnPct))
	tbl.addRow("Max Drawdown", ddStr)
	tbl.addRow("Avg Winner", fmt.Sprintf("$%.2f", s.AvgWinner))
	tbl.addRow("Avg Loser", fmt.Sprintf("$%.2f", s.AvgLoser))
	tbl.addRow("Risk/Reward", rrStr)
	tbl.addRow("Risk/Trade", fmt.Sprintf("%.2f%%", s.RiskPct))
	tbl.addRow("Stop", stopStr)
	if s.Regime != "" {
		tbl.addRow("Regime", s.Regime)
	}

	tbl.write(w, "   ")
}

type monthStats struct {
	month  string
	trades int
	wins   int
	losses int
	netPL  float64
}

func writeMonthlyTable(w io.Writer, s BacktestReportSummary) {
	byMonth := map[string]*monthStats{}
	var order []string

	for _, tr := range s.TradeDetails {
		m := shortMonth(tr.OpenTime)
		if _, ok := byMonth[m]; !ok {
			byMonth[m] = &monthStats{month: m}
			order = append(order, m)
		}
		ms := byMonth[m]
		ms.trades++
		ms.netPL += tr.PNL
		if tr.PNL > 0 {
			ms.wins++
		} else if tr.PNL < 0 {
			ms.losses++
		}
	}

	tbl := newOrgTable("Month", "Trades", "Wins", "Losses", "Win%", "Net P/L")
	tbl.setRight(1, 2, 3, 4, 5)

	for _, m := range order {
		ms := byMonth[m]
		winPct := 0.0
		if ms.trades > 0 {
			winPct = float64(ms.wins) / float64(ms.trades) * 100
		}
		tbl.addRow(
			ms.month,
			fmt.Sprintf("%d", ms.trades),
			fmt.Sprintf("%d", ms.wins),
			fmt.Sprintf("%d", ms.losses),
			fmt.Sprintf("%.1f%%", winPct),
			fmt.Sprintf("%+.2f", ms.netPL),
		)
	}
	tbl.write(w, "   ")
}

func writeTradeTable(w io.Writer, trades []BacktestReportTrade) {
	tbl := newOrgTable("#", "Side", "Open", "Close", "Entry", "Exit", "Units", "P/L")
	tbl.setRight(0, 4, 5, 6, 7)

	for i, tr := range trades {
		tbl.addRow(
			fmt.Sprintf("%d", i+1),
			titleASCII(tr.Side),
			shortDateTime(tr.OpenTime),
			shortDateTime(tr.CloseTime),
			fmt.Sprintf("%.5f", tr.OpenPrice),
			fmt.Sprintf("%.5f", tr.ClosePrice),
			fmt.Sprintf("%d", tr.Units),
			fmt.Sprintf("%+.2f", tr.PNL),
		)
	}
	tbl.write(w, "   ")
}

// ── orgTable ──────────────────────────────────────────────────────────────────

type orgTable struct {
	headers []string
	right   map[int]bool
	rows    [][]string
}

func newOrgTable(headers ...string) *orgTable {
	return &orgTable{headers: headers, right: map[int]bool{}}
}

func (t *orgTable) setRight(cols ...int) {
	for _, c := range cols {
		t.right[c] = true
	}
}

func (t *orgTable) addRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *orgTable) write(w io.Writer, indent string) {
	n := len(t.headers)

	// Compute column widths.
	widths := make([]int, n)
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i := 0; i < n && i < len(row); i++ {
			if l := len(row[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}

	writeRow := func(cells []string) {
		fmt.Fprint(w, indent+"|")
		for i := 0; i < n; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			if t.right[i] {
				fmt.Fprintf(w, " %*s |", widths[i], cell)
			} else {
				fmt.Fprintf(w, " %-*s |", widths[i], cell)
			}
		}
		fmt.Fprintln(w)
	}

	writeSep := func() {
		fmt.Fprint(w, indent+"|")
		for i, wid := range widths {
			fmt.Fprint(w, strings.Repeat("-", wid+2))
			if i < n-1 {
				fmt.Fprint(w, "+")
			}
		}
		fmt.Fprintln(w, "|")
	}

	writeRow(t.headers)
	writeSep()
	for _, row := range t.rows {
		writeRow(row)
	}
}

// ── date helpers ──────────────────────────────────────────────────────────────

func titleASCII(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func shortMonth(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

func shortDateTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("2006-01-02 15:04")
}
