package trader

import (
	"testing"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileBacktestComponents_WithExecutionDefaults_Success(t *testing.T) {
	t.Parallel()

	rc := RunConfig{
		Name: "run-1",
		Data: DataConfig{
			Instrument: "EURUSD",
			Timeframe:  "H1",
			From:       "2026-01-01",
			To:         "2026-01-10",
		},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}

	defaults := RunDefaults{StartingBalance: 10_000, RiskPct: 1.5}
	req, err := compileBacktestComponents(rc)
	require.NoError(t, err)
	require.NotNil(t, req)
	applyBacktestExecutionDefaults(req, rc, defaults)
	assert.Equal(t, "run-1", req.Name)
	assert.Equal(t, "EURUSD", req.Instrument)
	assert.Equal(t, market.H1, req.TimeRange.TF)
	assert.Equal(t, market.MoneyFromFloat(10_000), req.StartingBalance)
	assert.Equal(t, market.RateFromFloat(0.015), req.RiskPct)
	assert.Equal(t, hashBacktestConfig(rc, defaults), req.ConfigHash)
	require.NotNil(t, req.Strategy)
	assert.Equal(t, "Fake", req.Strategy.Name())
}

func TestCompileBacktestComponents_InvalidInputs(t *testing.T) {
	t.Parallel()

	badDates := RunConfig{
		Data:     DataConfig{From: "bad-date", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}
	_, err := compileBacktestComponents(badDates)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build backtest time range")

	badStrategy := RunConfig{
		Data:     DataConfig{From: "2026-01-01", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: strategy.StrategyConfig{Kind: "not-supported"},
	}
	_, err = compileBacktestComponents(badStrategy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build backtest strategy")
}

func TestCompileBacktestComponents_Success(t *testing.T) {
	t.Parallel()

	rc := RunConfig{
		Name: "run-1",
		Data: DataConfig{
			Instrument: "EURUSD",
			Timeframe:  "H1",
			From:       "2026-01-01",
			To:         "2026-01-10",
		},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}

	req, err := compileBacktestComponents(rc)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "run-1", req.Name)
	assert.Equal(t, "EURUSD", req.Instrument)
	assert.Equal(t, "candles", req.Source)
	assert.Equal(t, market.H1, req.TimeRange.TF)
	require.NotNil(t, req.Strategy)
	assert.Equal(t, "Fake", req.Strategy.Name())
	assert.Zero(t, req.StartingBalance)
	assert.Empty(t, req.ConfigHash)
}

func TestApplyBacktestExecutionDefaults(t *testing.T) {
	t.Parallel()

	rc := RunConfig{
		Name:     "run-1",
		Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-10"},
		Strategy: strategy.StrategyConfig{Kind: "fake"},
	}
	req := &BacktestRequest{Name: rc.Name, Instrument: rc.Data.Instrument}
	defaults := RunDefaults{
		StartingBalance: 10_000,
		RiskPct:         1.5,
		StopPips:        20,
		TakePips:        40,
		SlippagePips:    0.5,
		MaxSpreadPips:   2.0,
	}

	applyBacktestExecutionDefaults(req, rc, defaults)
	assert.Equal(t, hashBacktestConfig(rc, defaults), req.ConfigHash)
	assert.Equal(t, market.MoneyFromFloat(10_000), req.StartingBalance)
	assert.Equal(t, market.RateFromFloat(0.015), req.RiskPct)
	assert.Equal(t, market.PipsFromFloat(20), req.DefaultStopPips)
	assert.Equal(t, market.PipsFromFloat(40), req.DefaultTakePips)
	assert.Equal(t, market.PipsFromFloat(0.5), req.SlippagePips)
	assert.Equal(t, market.PipsFromFloat(2.0), req.MaxSpreadPips)
}

func TestCompileBacktests_SuccessAndDefaultsApplied(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Defaults: RunDefaults{
			StartingBalance: 12_500,
			RiskPct:         1.5,
			StopPips:        20,
			TakePips:        40,
			Source:          "oanda",
		},
		Runs: []RunConfig{
			{
				Name:     "a",
				Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-10"},
				Strategy: strategy.StrategyConfig{Kind: "fake"},
			},
			{
				Name:     "b",
				Data:     DataConfig{Instrument: "USDJPY", Timeframe: "D1", From: "2026-02-01", To: "2026-02-15"},
				Strategy: strategy.StrategyConfig{Kind: "noop"},
			},
		},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 2)

	for _, run := range runs {
		assert.Equal(t, market.MoneyFromFloat(12_500), run.Request.StartingBalance)
		assert.Equal(t, market.RateFromFloat(0.015), run.Request.RiskPct)
		assert.Equal(t, market.PipsFromFloat(20), run.Request.DefaultStopPips)
		assert.Equal(t, market.PipsFromFloat(40), run.Request.DefaultTakePips)
		assert.NotEmpty(t, run.ID)
	}

	assert.Equal(t, "oanda", runs[0].RunConfig.Data.Source)
}

func TestCompileBacktests_ErrorCases(t *testing.T) {
	t.Parallel()

	_, err := CompileBacktests(&Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 1 run")

	cfg := &Config{
		Runs: []RunConfig{{
			Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-10"},
			Strategy: strategy.StrategyConfig{Kind: "unknown"},
		}},
	}
	_, err = CompileBacktests(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build backtest strategy")
}

func TestBuildBacktestResult(t *testing.T) {
	t.Parallel()

	var nilRun *Backtest
	assert.Nil(t, nilRun.BuildBacktestResult(&execution.Account{}))

	run := &Backtest{
		Request: &BacktestRequest{StartingBalance: market.MoneyFromFloat(10_000)},
	}
	assert.Nil(t, run.BuildBacktestResult(nil))

	acct := &execution.Account{
		Balance: market.MoneyFromFloat(10_150),
		Equity:  market.MoneyFromFloat(10_200),
		Trades: []*execution.Trade{
			{PNL: market.MoneyFromFloat(100)},
			nil,
			{PNL: market.MoneyFromFloat(-25)},
			{PNL: 0},
		},
	}

	run.Request.TimeRange = market.TimeRange{Start: market.Timestamp(100), End: market.Timestamp(200)}
	res := run.BuildBacktestResult(acct)
	require.NotNil(t, res)
	assert.Equal(t, acct.Balance, res.Balance)
	assert.Equal(t, acct.Equity, res.Equity)
	assert.Equal(t, 3, res.Trades)
	assert.Equal(t, 1, res.Wins)
	assert.Equal(t, 1, res.Losses)
	assert.Equal(t, 1, res.Flat)
	assert.Equal(t, run.Request.StartingBalance, res.StartBalance)
	assert.Equal(t, market.MoneyFromFloat(100), res.GrossProfit)
	assert.Equal(t, market.MoneyFromFloat(-25), res.GrossLoss)
	assert.Equal(t, market.MoneyFromFloat(100), res.AvgWinner)
	assert.Equal(t, market.MoneyFromFloat(-25), res.AvgLoser)
	assert.Equal(t, market.RateFromFloat(4.0), res.ProfitFactor)
	assert.Equal(t, market.RateFromFloat(4.0), res.RR)
	assert.Equal(t, market.MoneyFromFloat(-25), res.MaxDrawdown)
	assert.Equal(t, market.RateFromFloat(-25.0/10_000.0), res.MaxDrawdownPct)
	assert.Equal(t, market.Timestamp(100), res.Start)
	assert.Equal(t, market.Timestamp(200), res.End)
	assert.Equal(t, acct.Balance-run.Request.StartingBalance, res.NetPL)
	assert.Equal(t, market.RateFromFloat(1.0/3.0), res.WinRate)
	assert.Equal(t, market.RateFromFloat(res.NetPL.Float64()/run.Request.StartingBalance.Float64()), res.ReturnPct)
	assert.Same(t, res, run.Result)
}

func TestSummary_AndFormatBacktestSummaryTime(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", formatBacktestSummaryTime(0))
	assert.Equal(t, "1970-01-01T00:00:01Z", formatBacktestSummaryTime(market.Timestamp(1)))

	var nilRun *Backtest
	assert.Equal(t, BacktestReportSummary{}, nilRun.Summary())

	fake, err := strategy.GetStrategy(strategy.StrategyConfig{Kind: "fake"})
	require.NoError(t, err)

	run := &Backtest{
		Request: &BacktestRequest{
			Name:            "summary-run",
			Instrument:      "EURUSD",
			Strategy:        fake,
			TimeRange:       market.TimeRange{Start: market.Timestamp(1), End: market.Timestamp(3601), TF: market.H1},
			StartingBalance: market.MoneyFromFloat(10_000),
			RiskPct:         market.RateFromFloat(0.01),
			DefaultStopPips: market.PipsFromFloat(20),
		},
		State: &BacktestRun{},
		Result: &BacktestResult{
			Start:        market.Timestamp(1),
			End:          market.Timestamp(3601),
			StartBalance: market.MoneyFromFloat(10_000),
			Trades:       10,
			Wins:         6,
			Losses:       4,
			Balance:      market.MoneyFromFloat(10_250),
			NetPL:        market.MoneyFromFloat(250),
			ReturnPct:    market.RateFromFloat(0.025),
			WinRate:      market.RateFromFloat(0.6),
			AvgWinner:    market.MoneyFromFloat(150),
			AvgLoser:     market.MoneyFromFloat(-75),
			RR:           market.RateFromFloat(2.0),
			MaxDrawdown:  market.MoneyFromFloat(-80),
		},
	}

	s := run.Summary()
	assert.Equal(t, "summary-run", s.Name)
	assert.Equal(t, "Fake", s.Strategy)
	assert.Equal(t, "EURUSD", s.Instrument)
	assert.Equal(t, "h1", s.Timeframe)
	assert.Equal(t, "1970-01-01T00:00:01Z", s.Start)
	assert.Equal(t, "1970-01-01T01:00:01Z", s.End)
	assert.Equal(t, 10, s.Trades)
	assert.Equal(t, 6, s.Wins)
	assert.Equal(t, 4, s.Losses)
	assert.InDelta(t, 10_000.0, s.StartBalance, 1e-9)
	assert.InDelta(t, 10_250.0, s.EndBalance, 1e-9)
	assert.InDelta(t, 250.0, s.NetPL, 1e-9)
	assert.InDelta(t, 2.5, s.ReturnPct, 1e-9)
	assert.InDelta(t, 60.0, s.WinRate, 1e-9)
	assert.InDelta(t, -80.0, s.MaxDrawdown, 1e-9)
	assert.InDelta(t, 150.0, s.AvgWinner, 1e-9)
	assert.InDelta(t, -75.0, s.AvgLoser, 1e-9)
	assert.InDelta(t, 2.0, s.RR, 1e-9)
	assert.InDelta(t, 1.0, s.RiskPct, 1e-9)
	assert.Equal(t, "", s.Stop) // Fake strategy returns no stop description
}
