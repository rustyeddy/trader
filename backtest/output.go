package backtest

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/types"
)

func PrintBacktestRun(w io.Writer, r BacktestRun) {
	fmt.Fprintln(w, "==================================================")
	fmt.Fprintln(w, " Backtest Result")
	fmt.Fprintln(w, "==================================================")

	fmt.Fprintf(w, "Run ID:        %s\n", r.RunID)
	fmt.Fprintf(w, "Run Name:      %s\n", r.Name)
	fmt.Fprintf(w, "Kind:          %s\n", r.Kind)
	fmt.Fprintf(w, "Created:       %s\n", r.Created.Time())
	fmt.Fprintf(w, "Strategy:      %s\n", r.Strategy)
	fmt.Fprintf(w, "Instrument:    %s\n", r.Instrument)
	fmt.Fprintf(w, "Timeframe:     %s\n", r.Timeframe)
	fmt.Fprintf(w, "Dataset:       %s\n", r.Dataset)

	if r.GitCommit != "" {
		fmt.Fprintf(w, "Git Commit:    %s\n", r.GitCommit)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Period")
	fmt.Fprintln(w, "--------------------------------------------------")
	fmt.Fprintf(w, "Start:         %s\n", r.Start.Time())
	fmt.Fprintf(w, "End:           %s\n", r.End.Time())

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Strategy Configuration")
	fmt.Fprintln(w, "--------------------------------------------------")
	fmt.Fprintf(w, "Risk per Trade: %.2f%%\n", float64(r.RiskPct)*100/float64(types.RateScale))
	fmt.Fprintf(w, "Stop Loss:     %.1f pips\n", float64(r.StopPips))
	fmt.Fprintf(w, "Risk/Reward:   %.2f\n", float64(r.RR)/float64(types.RateScale))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Trade Statistics")
	fmt.Fprintln(w, "--------------------------------------------------")
	fmt.Fprintf(w, "Trades:        %d\n", r.Trades)
	fmt.Fprintf(w, "Wins:          %d\n", r.Wins)
	fmt.Fprintf(w, "Losses:        %d\n", r.Losses)
	fmt.Fprintf(w, "Win Rate:      %.2f%%\n", float64(r.WinRate)*100/float64(types.RateScale))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Account Performance")
	fmt.Fprintln(w, "--------------------------------------------------")
	fmt.Fprintf(w, "Start Balance: %.2f\n", r.StartBalance.Float64())
	fmt.Fprintf(w, "End Balance:   %.2f\n", r.EndBalance.Float64())
	fmt.Fprintf(w, "Net P/L:       %.2f\n", r.NetPL.Float64())
	fmt.Fprintf(w, "Return:        %.2f%%\n", float64(r.ReturnPct)*100/float64(types.RateScale))

	if r.ProfitFactor > 0 {
		fmt.Fprintf(w, "Profit Factor: %.2f\n", float64(r.ProfitFactor)/float64(types.RateScale))
	}
	if r.MaxDDPct > 0 {
		fmt.Fprintf(w, "Max Drawdown:  %.2f%%\n", float64(r.MaxDDPct)*100/float64(types.RateScale))
	}

	if r.EquityPNG != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Equity Curve:  %s\n", r.EquityPNG)
	}

	if r.OrgPath != "" {
		fmt.Fprintf(w, "Org Report:    %s\n", r.OrgPath)
	}

	if len(r.Notes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Observations")
		fmt.Fprintln(w, "--------------------------------------------------")
		for _, note := range r.Notes {
			fmt.Fprintf(w, "- %s\n", note)
		}
	}

	if len(r.NextActions) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next Actions")
		fmt.Fprintln(w, "--------------------------------------------------")
		for _, action := range r.NextActions {
			fmt.Fprintf(w, "- [ ] %s\n", action)
		}
	}

	fmt.Fprintln(w)
}
