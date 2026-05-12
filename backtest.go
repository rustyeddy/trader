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

	strategy, err := GetStrategy(cfg.Strategy.Kind)
	if err != nil {
		return nil
	}

	return &BacktestRequest{
		Name:       cfg.Name,
		Instrument: cfg.Data.Instrument,
		Strategy:   strategy,
		TimeRange:  tr,
	}
}

type BacktestRequest struct {
	Name string

	StartingBalance Money
	RiskPct         Rate

	DefaultStopPips Pips
	DefaultTakePips Pips

	Instrument string
	Strategy
	TimeRange
}

func (run *Backtest) BuildBacktestResult(acct *Account) *BacktestResult {
	if run == nil || acct == nil {
		return nil
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
		StopPips:     int32(run.DefaultStopPips),
	}
}
