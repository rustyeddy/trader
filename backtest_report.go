package trader

import (
	"fmt"
	"io"
	"strings"
)

// BacktestReportSummary is a normalized machine-readable summary used for
// committed regression baselines and generated comparison artifacts.
// The Config and ConfigHash fields make every report self-describing: you can
// open any JSON file and see exactly what params produced it.
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

	Stop      string `json:"stop"`
	Regime    string `json:"regime"`
	MaxSpread string `json:"max_spread,omitempty"`
	Slippage  string `json:"slippage,omitempty"`

	// Execution cost stats
	AvgSpreadPips  float64 `json:"avg_spread_pips"`
	SpreadFiltered int     `json:"spread_filtered"`
	RR             float64 `json:"rr"`
	MaxDrawdown    float64 `json:"max_drawdown"` // largest peak-to-trough drop in dollars (negative)
	AvgWinner      float64 `json:"avg_winner"`
	AvgLoser       float64 `json:"avg_loser"` // negative

	TradeDetails []BacktestReportTrade `json:"trade_details,omitempty"`

	// Provenance — always populated; links this report back to its origin.
	ConfigHash  string    `json:"config_hash"`  // 8-char SHA256 prefix of the run config params
	GeneratedAt string    `json:"generated_at"` // RFC3339 UTC timestamp of when the run completed
	Config      RunConfig `json:"config"`       // full config snapshot that produced this result
}

// BacktestReportTrade is a JSON-serialisable record of a single closed trade
// used inside BacktestReportSummary.TradeDetails.
type BacktestReportTrade struct {
	ID              string  `json:"id"`
	Instrument      string  `json:"instrument"`
	Side            string  `json:"side"`
	Units           int64   `json:"units"`
	OpenPrice       float64 `json:"open_price"`
	ClosePrice      float64 `json:"close_price"`
	OpenTime        string  `json:"open_time"`
	CloseTime       string  `json:"close_time"`
	PNL             float64 `json:"pnl"`
	StopPrice       float64 `json:"stop_price,omitempty"`
	TakeProfitPrice float64 `json:"take_profit_price,omitempty"`
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

	stopStr := s.Stop
	if stopStr == "" {
		stopStr = "—"
	}
	rrStr := "—"
	if s.RR > 0 {
		rrStr = fmt.Sprintf("%.1f", s.RR)
	}

	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "  %-48s\n", s.Strategy)
	fmt.Fprintf(w, "  %s %s  %s → %s\n", s.Instrument, strings.ToUpper(s.Timeframe), start, end)
	ddStr := "—"
	if s.MaxDrawdown < 0 {
		ddStr = fmt.Sprintf("-$%.2f", -s.MaxDrawdown)
	}

	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "  Trades : %d   Wins: %d (%.1f%%)   Losses: %d\n",
		s.Trades, s.Wins, s.WinRate, s.Losses)
	fmt.Fprintf(w, "  Balance: $%.2f → $%.2f   (%s$%.2f / %s%.2f%%)\n",
		s.StartBalance, s.EndBalance, sign, absNetPL, sign, absRetPct)
	fmt.Fprintf(w, "  Drawdown: %s   Avg W: $%.2f   Avg L: $%.2f\n",
		ddStr, s.AvgWinner, s.AvgLoser)
	regimeStr := ""
	if s.Regime != "" {
		regimeStr = fmt.Sprintf("   Regime: %s", s.Regime)
	}
	maxSpreadStr := ""
	if s.MaxSpread != "" {
		maxSpreadStr = fmt.Sprintf("   MaxSpread: %s", s.MaxSpread)
	}
	fmt.Fprintf(w, "  Risk   : %.2f%%   Stop: %s   RR: %s%s%s\n",
		s.RiskPct, stopStr, rrStr, regimeStr, maxSpreadStr)
	if s.AvgSpreadPips > 0 || s.SpreadFiltered > 0 || s.Slippage != "" {
		slipStr := ""
		if s.Slippage != "" {
			slipStr = fmt.Sprintf("   Slip: %s", s.Slippage)
		}
		filtStr := ""
		if s.SpreadFiltered > 0 {
			filtStr = fmt.Sprintf("   Filtered: %d", s.SpreadFiltered)
		}
		fmt.Fprintf(w, "  AvgSpread: %.2fp%s%s\n", s.AvgSpreadPips, slipStr, filtStr)
	}
	fmt.Fprintln(w, bar)
}
