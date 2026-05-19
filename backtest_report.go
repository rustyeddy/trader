package trader

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// BacktestReportSummary is a normalized machine-readable summary used for
// committed regression baselines and generated comparison artifacts.
type BacktestReportSummary struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Strategy   string `json:"strategy"`
	Instrument string `json:"instrument"`
	Timeframe  string `json:"timeframe"`
	Dataset    string `json:"dataset"`
	Start      string `json:"start"`
	End        string `json:"end"`

	Trades int `json:"trades"`
	Wins   int `json:"wins"`
	Losses int `json:"losses"`

	StartBalance float64 `json:"start_balance"`
	EndBalance   float64 `json:"end_balance"`
	NetPL        float64 `json:"net_pl"`

	// Stored as human-friendly percentages, e.g. 12.34 means 12.34%
	ReturnPct float64 `json:"return_pct"`
	WinRate   float64 `json:"win_rate"`
	RiskPct   float64 `json:"risk_pct"`

	StopPips int32   `json:"stop_pips"`
	RR       float64 `json:"rr"`

	TradeDetails []BacktestReportTrade `json:"trade_details,omitempty"`
}

type BacktestReportTrade struct {
	ID         string  `json:"id"`
	Instrument string  `json:"instrument"`
	Side       string  `json:"side"`
	Units      int64   `json:"units"`
	OpenPrice  float64 `json:"open_price"`
	ClosePrice float64 `json:"close_price"`
	OpenTime   string  `json:"open_time"`
	CloseTime  string  `json:"close_time"`
	PNL        float64 `json:"pnl"`
}

func NewBacktestReportSummary(r *BacktestResult) BacktestReportSummary {
	return BacktestReportSummary{}
	// return BacktestReportSummary{
	// 	Name:         r.Name,
	// 	Kind:         r.Kind,
	// 	Strategy:     r.Strategy,
	// 	Instrument:   r.Instrument,
	// 	Timeframe:    r.Timeframe,
	// 	Dataset:      r.Dataset,
	// 	Start:        formatBacktestSummaryTime(r.Start),
	// 	End:          formatBacktestSummaryTime(r.End),
	// 	Trades:       r.Trades,
	// 	Wins:         r.Wins,
	// 	Losses:       r.Losses,
	// 	StartBalance: r.StartBalance.Float64(),
	// 	EndBalance:   r.EndBalance.Float64(),
	// 	NetPL:        r.NetPL.Float64(),
	// 	ReturnPct:    r.ReturnPct.Float64() * 100.0,
	// 	WinRate:      r.WinRate.Float64() * 100.0,
	// 	RiskPct:      r.RiskPct.Float64() * 100.0,
	// 	StopPips:     int32(r.StopPips),
	// 	RR:           r.RR.Float64(),
	// }
}

// PrintSummary writes a human-readable backtest report to w.
func PrintSummary(w io.Writer, s BacktestReportSummary) {
	const width = 52
	bar := strings.Repeat("─", width)

	start := s.Start
	if len(start) >= 10 {
		start = start[:10]
	}
	end := s.End
	if len(end) >= 10 {
		end = end[:10]
	}

	sign := "+"
	if s.NetPL < 0 {
		sign = "-"
	}
	absNetPL := s.NetPL
	if absNetPL < 0 {
		absNetPL = -absNetPL
	}
	absRetPct := s.ReturnPct
	if absRetPct < 0 {
		absRetPct = -absRetPct
	}

	stopStr := "—"
	if s.StopPips > 0 {
		stopStr = fmt.Sprintf("%d pips", s.StopPips)
	}
	rrStr := "—"
	if s.RR > 0 {
		rrStr = fmt.Sprintf("%.1f", s.RR)
	}

	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "  %-48s\n", s.Strategy)
	fmt.Fprintf(w, "  %s %s  %s → %s\n", s.Instrument, strings.ToUpper(s.Timeframe), start, end)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "  Trades : %d   Wins: %d (%.1f%%)   Losses: %d\n",
		s.Trades, s.Wins, s.WinRate, s.Losses)
	fmt.Fprintf(w, "  Balance: $%.2f → $%.2f   (%s$%.2f / %s%.2f%%)\n",
		s.StartBalance, s.EndBalance, sign, absNetPL, sign, absRetPct)
	fmt.Fprintf(w, "  Risk   : %.2f%%   Stop: %s   RR: %s\n",
		s.RiskPct, stopStr, rrStr)
	fmt.Fprintln(w, bar)
}

func formatBacktestSummaryTime(ts Timestamp) string {
	if ts == 0 {
		return ""
	}
	return ts.Time().UTC().Format(time.RFC3339)
}
