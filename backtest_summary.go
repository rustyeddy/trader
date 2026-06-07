package trader

import (
	"fmt"
	"time"
)

// Summary builds a fully-populated BacktestReportSummary from the run's
// request and result fields. It is safe to call after BuildBacktestResult.
// Returns a zero-value summary if any required field is nil.
func (run *Backtest) Summary() BacktestReportSummary {
	if run == nil || run.Request == nil || run.Result == nil || run.Request.Strategy == nil {
		return BacktestReportSummary{}
	}

	var trades []BacktestReportTrade
	for _, tr := range run.State.GetTrades() {
		if tr == nil {
			continue
		}

		trades = append(trades, BacktestReportTrade{
			ID:              tr.ID,
			Instrument:      tr.Instrument,
			Side:            tr.Side.String(),
			Units:           int64(tr.Units),
			OpenPrice:       tr.EntryPrice.Float64(),
			ClosePrice:      tr.ExitPrice.Float64(),
			OpenTime:        formatBacktestSummaryTime(tr.EntryTime),
			CloseTime:       formatBacktestSummaryTime(tr.ExitTime),
			PNL:             tr.PNL.Float64(),
			StopPrice:       tr.Stop.Float64(),
			TakeProfitPrice: tr.Take.Float64(),
		})
	}

	maxDD, avgWinner, avgLoser := computeTradeStats(trades)
	rr := 0.0
	if avgLoser != 0 {
		rr = avgWinner / -avgLoser
	}

	avgSpreadPips, spreadFiltered := executionCostStats(run)

	return BacktestReportSummary{
		Name:       run.Request.Name,
		Strategy:   run.Request.Strategy.Name(),
		Instrument: run.Request.Instrument,
		Timeframe:  run.Request.TimeRange.TF.String(),
		Start:      formatBacktestSummaryTime(run.Request.TimeRange.Start),
		End:        formatBacktestSummaryTime(run.Request.TimeRange.End),

		Trades:         run.Result.Trades,
		Wins:           run.Result.Wins,
		Losses:         run.Result.Losses,
		StartBalance:   run.Request.StartingBalance.Float64(),
		EndBalance:     run.Result.Balance.Float64(),
		NetPL:          run.Result.NetPL.Float64(),
		ReturnPct:      run.Result.ReturnPct.Float64() * 100,
		WinRate:        run.Result.WinRate.Float64() * 100,
		RiskPct:        run.Request.RiskPct.Float64() * 100,
		Stop:           stopDescription(run),
		Regime:         regimeDescription(run),
		MaxSpread:      maxSpreadDescription(run),
		Slippage:       slippageDescription(run),
		AvgSpreadPips:  avgSpreadPips,
		SpreadFiltered: spreadFiltered,
		MaxDrawdown:    maxDD,
		AvgWinner:      avgWinner,
		AvgLoser:       avgLoser,
		RR:             rr,

		TradeDetails: trades,

		ConfigHash:  run.Request.ConfigHash,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      run.RunConfig,
	}
}

// computeTradeStats derives max drawdown, avg winner, and avg loser from the trade list.
// MaxDrawdown is the largest peak-to-trough drop in cumulative P/L (returned as negative).
func computeTradeStats(trades []BacktestReportTrade) (maxDrawdown, avgWinner, avgLoser float64) {
	var running, peak float64
	var winSum, lossSum float64
	var winN, lossN int

	for _, tr := range trades {
		running += tr.PNL
		if running > peak {
			peak = running
		}
		if drop := peak - running; drop > -maxDrawdown {
			maxDrawdown = -drop
		}
		if tr.PNL > 0 {
			winSum += tr.PNL
			winN++
		} else if tr.PNL < 0 {
			lossSum += tr.PNL
			lossN++
		}
	}

	if winN > 0 {
		avgWinner = winSum / float64(winN)
	}
	if lossN > 0 {
		avgLoser = lossSum / float64(lossN)
	}
	return
}

// regimeDescription returns the regime filter's name for display in the
// summary, or an empty string when no filter is configured.
func regimeDescription(run *Backtest) string {
	if run != nil && run.Request != nil && run.Request.Regime != nil {
		if name := run.Request.Regime.Name(); name != "" {
			return name
		}
	}
	return ""
}

// slippageDescription returns a formatted slippage label (e.g. "1.5p") or
// an empty string when slippage is zero.
func slippageDescription(run *Backtest) string {
	if run == nil || run.Request == nil || run.Request.SlippagePips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.Request.SlippagePips.Float64())
}

// executionCostStats returns the average spread (in pips) across accepted
// opens and the number of opens that were suppressed by the max-spread filter.
func executionCostStats(run *Backtest) (avgSpreadPips float64, spreadFiltered int) {
	if run == nil || run.State == nil || run.Request == nil {
		return 0, 0
	}
	spreadFiltered = run.State.SpreadFiltered
	if run.State.SpreadOpened == 0 {
		return 0, spreadFiltered
	}
	inst := GetInstrument(run.Request.Instrument)
	if inst == nil {
		return 0, spreadFiltered
	}
	unitsPerPip := float64(inst.PriceUnitsPerPip())
	avgSpreadPips = float64(run.State.SpreadSum) / float64(run.State.SpreadOpened) / unitsPerPip
	return avgSpreadPips, spreadFiltered
}

// maxSpreadDescription returns a formatted max-spread label (e.g. "2.0p") or
// an empty string when no spread filter is configured.
func maxSpreadDescription(run *Backtest) string {
	if run == nil || run.Request == nil || run.Request.MaxSpreadPips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.Request.MaxSpreadPips.Float64())
}

// stopDescription returns the stop label for the summary, preferring the exit
// strategy's name when one is configured, then falling back to the entry
// strategy's StopDescription.
func stopDescription(run *Backtest) string {
	if run == nil || run.Request == nil || run.Request.Strategy == nil {
		return ""
	}
	if run.Request.Exit != nil {
		if name := run.Request.Exit.Name(); name != "" {
			return name
		}
	}
	return run.Request.Strategy.StopDescription()
}

// formatBacktestSummaryTime formats a Timestamp as RFC3339 UTC, or "" for zero.
func formatBacktestSummaryTime(ts Timestamp) string {
	if ts == 0 {
		return ""
	}
	return ts.Time().UTC().Format(time.RFC3339)
}
