package trader

import "fmt"

type Backtest struct {
	ID string

	*BacktestRequest
	*BacktestRun
	*BacktestResult

	// Trades []Trade
}

func GetBacktests(cfg *Config) ([]Backtest, error) {
	runs := make([]Backtest, 0, len(cfg.Runs))
	for _, runcfg := range cfg.Runs {
		run := &Backtest{
			ID:          NewULID(),
			BacktestRun: &BacktestRun{},
		}

		req := newBacktestReq(runcfg)
		if req == nil {
			return nil, fmt.Errorf("failed to create BacktestRequest from config")
		}

		req.StartingBalance = MoneyFromFloat(cfg.Defaults.StartingBalance)
		req.RiskPct = RateFromFloat(cfg.Defaults.RiskPct / 100.0)
		req.DefaultStopPips = pipsFromFloat(float64(cfg.Defaults.StopPips))
		req.DefaultTakePips = pipsFromFloat(float64(cfg.Defaults.TakePips))
		req.SlippagePips = pipsFromFloat(cfg.Defaults.SlippagePips)
		req.MaxSpreadPips = pipsFromFloat(cfg.Defaults.MaxSpreadPips)

		run.BacktestRequest = req
		runs = append(runs, *run)
	}
	if len(runs) < 1 {
		return nil, fmt.Errorf("regression config must resolve to exactly 1 run, got %d", len(runs))
	}
	return runs, nil
}

func newBacktestReq(cfg RunConfig) *BacktestRequest {

	tr, err := timeRangeFromStrings(cfg.Data.From, cfg.Data.To, cfg.Data.Timeframe)
	if err != nil {
		fmt.Printf("Failed to create Backtest Request")
		return nil
	}

	strategy, err := GetStrategy(cfg.Strategy)
	if err != nil {
		return nil
	}

	scale := Scale6(PriceScale)
	exit, err := GetExitStrategy(cfg.Exit, scale)
	if err != nil {
		fmt.Printf("failed to build exit strategy: %v\n", err)
		return nil
	}

	regime, err := GetRegimeFilter(cfg.Regime, scale)
	if err != nil {
		fmt.Printf("failed to build regime filter: %v\n", err)
		return nil
	}

	source := firstNonEmpty(cfg.Data.Source, "candles")
	return &BacktestRequest{
		Name:       cfg.Name,
		Source:     source,
		Instrument: cfg.Data.Instrument,
		Strategy:   strategy,
		Exit:       exit,
		Regime:     regime,
		TimeRange:  tr,
	}
}

type BacktestRequest struct {
	Name string

	StartingBalance Money
	RiskPct         Rate

	DefaultStopPips Pips
	DefaultTakePips Pips
	SlippagePips    Pips
	MaxSpreadPips   Pips

	Source     string
	Instrument string
	Strategy
	Exit   ExitStrategy
	Regime RegimeFilter
	TimeRange
}

func (run *Backtest) BuildBacktestResult(acct *Account) *BacktestResult {
	if run == nil || acct == nil {
		return nil
	}
	if run.BacktestRun == nil {
		run.BacktestRun = &BacktestRun{}
	}

	run.BacktestRun.Trades = append(run.BacktestRun.Trades[:0], acct.Trades...)

	res := &BacktestResult{
		Balance: acct.Balance,
		Equity:  acct.Equity,
		Trades:  len(acct.Trades),
		Start:   run.TimeRange.Start,
		End:     run.TimeRange.End,
	}

	for _, tr := range acct.Trades {
		if tr == nil {
			continue
		}
		switch {
		case tr.PNL > 0:
			res.Wins++
		case tr.PNL < 0:
			res.Losses++
		case tr.PNL == 0:
			res.Flat++
		}
	}

	res.NetPL = acct.Balance - run.StartingBalance
	if run.StartingBalance != 0 {
		res.ReturnPct = RateFromFloat(res.NetPL.Float64() / run.StartingBalance.Float64())
	}
	if res.Trades > 0 {
		res.WinRate = RateFromFloat(float64(res.Wins) / float64(res.Trades))
	}
	run.BacktestResult = res
	return run.BacktestResult

}

func (run *Backtest) Summary() BacktestReportSummary {
	if run == nil || run.BacktestRequest == nil || run.BacktestResult == nil || run.Strategy == nil {
		return BacktestReportSummary{}
	}

	var trades []BacktestReportTrade
	for _, tr := range run.BacktestRun.GetTrades() {
		if tr == nil {
			continue
		}

		trades = append(trades, BacktestReportTrade{
			ID:         tr.ID,
			Instrument: tr.Instrument,
			Side:       tr.Side.String(),
			Units:      int64(tr.Units),
			OpenPrice:  tr.EntryPrice.Float64(),
			ClosePrice: tr.ExitPrice.Float64(),
			OpenTime:   formatBacktestSummaryTime(tr.EntryTime),
			CloseTime:  formatBacktestSummaryTime(tr.ExitTime),
			PNL:        tr.PNL.Float64(),
		})
	}

	maxDD, avgWinner, avgLoser := computeTradeStats(trades)
	rr := 0.0
	if avgLoser != 0 {
		rr = avgWinner / -avgLoser
	}

	avgSpreadPips, spreadFiltered := executionCostStats(run)

	return BacktestReportSummary{
		Name:       run.Name,
		Strategy:   run.Strategy.Name(),
		Instrument: run.Instrument,
		Timeframe:  run.TimeRange.TF.String(),
		Start:      formatBacktestSummaryTime(run.TimeRange.Start),
		End:        formatBacktestSummaryTime(run.TimeRange.End),

		Trades:       run.BacktestResult.Trades,
		Wins:         run.BacktestResult.Wins,
		Losses:       run.BacktestResult.Losses,
		StartBalance: run.StartingBalance.Float64(),
		EndBalance:   run.BacktestResult.Balance.Float64(),
		NetPL:        run.BacktestResult.NetPL.Float64(),
		ReturnPct:    run.BacktestResult.ReturnPct.Float64() * 100,
		WinRate:      run.BacktestResult.WinRate.Float64() * 100,
		RiskPct:      run.RiskPct.Float64() * 100,
		Stop:         stopDescription(run),
		Regime:       regimeDescription(run),
		MaxSpread:    maxSpreadDescription(run),
		Slippage:     slippageDescription(run),
		AvgSpreadPips:  avgSpreadPips,
		SpreadFiltered: spreadFiltered,
		MaxDrawdown:  maxDD,
		AvgWinner:    avgWinner,
		AvgLoser:     avgLoser,
		RR:           rr,

		TradeDetails: trades,
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

// stopDescription builds the stop label for the summary, preferring the exit
// strategy's name when one is configured, then falling back to the entry
// strategy's StopDescription.
func regimeDescription(run *Backtest) string {
	if run.Regime != nil {
		if name := run.Regime.Name(); name != "" {
			return name
		}
	}
	return ""
}

func slippageDescription(run *Backtest) string {
	if run.SlippagePips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.SlippagePips.Float64())
}

func executionCostStats(run *Backtest) (avgSpreadPips float64, spreadFiltered int) {
	if run.BacktestRun == nil {
		return 0, 0
	}
	spreadFiltered = run.BacktestRun.SpreadFiltered
	if run.BacktestRun.SpreadOpened == 0 {
		return 0, spreadFiltered
	}
	inst := GetInstrument(run.Instrument)
	if inst == nil {
		return 0, spreadFiltered
	}
	unitsPerPip := float64(inst.PriceUnitsPerPip())
	avgSpreadPips = float64(run.SpreadSum) / float64(run.SpreadOpened) / unitsPerPip
	return avgSpreadPips, spreadFiltered
}

func maxSpreadDescription(run *Backtest) string {
	if run.MaxSpreadPips == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fp", run.MaxSpreadPips.Float64())
}

func stopDescription(run *Backtest) string {
	if run.Exit != nil {
		if name := run.Exit.Name(); name != "" {
			return name
		}
	}
	return run.Strategy.StopDescription()
}
