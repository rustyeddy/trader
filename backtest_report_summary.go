package trader

import "time"

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
}

func NewBacktestReportSummary(r BacktestRunVars) BacktestReportSummary {
	return BacktestReportSummary{
		Name:         r.Name,
		Kind:         r.Kind,
		Strategy:     r.Strategy,
		Instrument:   r.Instrument,
		Timeframe:    r.Timeframe,
		Dataset:      r.Dataset,
		Start:        formatBacktestSummaryTime(r.Start),
		End:          formatBacktestSummaryTime(r.End),
		Trades:       r.Trades,
		Wins:         r.Wins,
		Losses:       r.Losses,
		StartBalance: r.StartBalance.Float64(),
		EndBalance:   r.EndBalance.Float64(),
		NetPL:        r.NetPL.Float64(),
		ReturnPct:    r.ReturnPct.Float64() * 100.0,
		WinRate:      r.WinRate.Float64() * 100.0,
		RiskPct:      r.RiskPct.Float64() * 100.0,
		StopPips:     int32(r.StopPips),
		RR:           r.RR.Float64(),
	}
}

func formatBacktestSummaryTime(ts Timestamp) string {
	if ts == 0 {
		return ""
	}
	return ts.Time().UTC().Format(time.RFC3339)
}
