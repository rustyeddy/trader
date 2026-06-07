package trader

import (
	"testing"

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
		Strategy: StrategyConfig{Kind: "fake"},
	}

	defaults := RunDefaults{StartingBalance: 10_000, RiskPct: 1.5}
	req, err := compileBacktestComponents(rc)
	require.NoError(t, err)
	require.NotNil(t, req)
	applyBacktestExecutionDefaults(req, rc, defaults)
	assert.Equal(t, "run-1", req.Name)
	assert.Equal(t, "EURUSD", req.Instrument)
	assert.Equal(t, H1, req.TimeRange.TF)
	assert.Equal(t, MoneyFromFloat(10_000), req.StartingBalance)
	assert.Equal(t, RateFromFloat(0.015), req.RiskPct)
	assert.Equal(t, hashBacktestConfig(rc, defaults), req.ConfigHash)
	require.NotNil(t, req.Strategy)
	assert.Equal(t, "Fake", req.Strategy.Name())
}

func TestCompileBacktestComponents_InvalidInputs(t *testing.T) {
	t.Parallel()

	badDates := RunConfig{
		Data:     DataConfig{From: "bad-date", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: StrategyConfig{Kind: "fake"},
	}
	_, err := compileBacktestComponents(badDates)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build backtest time range")

	badStrategy := RunConfig{
		Data:     DataConfig{From: "2026-01-01", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: StrategyConfig{Kind: "not-supported"},
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
		Strategy: StrategyConfig{Kind: "fake"},
	}

	req, err := compileBacktestComponents(rc)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "run-1", req.Name)
	assert.Equal(t, "EURUSD", req.Instrument)
	assert.Equal(t, "candles", req.Source)
	assert.Equal(t, H1, req.TimeRange.TF)
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
		Strategy: StrategyConfig{Kind: "fake"},
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
	assert.Equal(t, MoneyFromFloat(10_000), req.StartingBalance)
	assert.Equal(t, RateFromFloat(0.015), req.RiskPct)
	assert.Equal(t, pipsFromFloat(20), req.DefaultStopPips)
	assert.Equal(t, pipsFromFloat(40), req.DefaultTakePips)
	assert.Equal(t, pipsFromFloat(0.5), req.SlippagePips)
	assert.Equal(t, pipsFromFloat(2.0), req.MaxSpreadPips)
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
				Strategy: StrategyConfig{Kind: "fake"},
			},
			{
				Name:     "b",
				Data:     DataConfig{Instrument: "USDJPY", Timeframe: "D1", From: "2026-02-01", To: "2026-02-15"},
				Strategy: StrategyConfig{Kind: "noop"},
			},
		},
	}

	runs, err := CompileBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 2)

	for _, run := range runs {
		assert.Equal(t, MoneyFromFloat(12_500), run.Request.StartingBalance)
		assert.Equal(t, RateFromFloat(0.015), run.Request.RiskPct)
		assert.Equal(t, pipsFromFloat(20), run.Request.DefaultStopPips)
		assert.Equal(t, pipsFromFloat(40), run.Request.DefaultTakePips)
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
			Strategy: StrategyConfig{Kind: "unknown"},
		}},
	}
	_, err = CompileBacktests(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build backtest strategy")
}

func TestBuildBacktestResult(t *testing.T) {
	t.Parallel()

	var nilRun *Backtest
	assert.Nil(t, nilRun.BuildBacktestResult(&Account{}))

	run := &Backtest{
		Request: &BacktestRequest{StartingBalance: MoneyFromFloat(10_000)},
	}
	assert.Nil(t, run.BuildBacktestResult(nil))

	acct := &Account{
		Balance: MoneyFromFloat(10_150),
		Equity:  MoneyFromFloat(10_200),
		Trades: []*Trade{
			{PNL: MoneyFromFloat(100)},
			nil,
			{PNL: MoneyFromFloat(-25)},
			{PNL: 0},
		},
	}

	run.Request.TimeRange = TimeRange{Start: Timestamp(100), End: Timestamp(200)}
	res := run.BuildBacktestResult(acct)
	require.NotNil(t, res)
	assert.Equal(t, acct.Balance, res.Balance)
	assert.Equal(t, acct.Equity, res.Equity)
	assert.Equal(t, 4, res.Trades)
	assert.Equal(t, 1, res.Wins)
	assert.Equal(t, 1, res.Losses)
	assert.Equal(t, Timestamp(100), res.Start)
	assert.Equal(t, Timestamp(200), res.End)
	assert.Equal(t, acct.Balance-run.Request.StartingBalance, res.NetPL)
	assert.Equal(t, RateFromFloat(1.0/4.0), res.WinRate)
	assert.Equal(t, RateFromFloat(res.NetPL.Float64()/run.Request.StartingBalance.Float64()), res.ReturnPct)
	assert.Same(t, res, run.Result)
}

func TestSummary_AndFormatBacktestSummaryTime(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", formatBacktestSummaryTime(0))
	assert.Equal(t, "1970-01-01T00:00:01Z", formatBacktestSummaryTime(Timestamp(1)))

	var nilRun *Backtest
	assert.Equal(t, BacktestReportSummary{}, nilRun.Summary())

	fake, err := GetStrategy(StrategyConfig{Kind: "fake"})
	require.NoError(t, err)

	run := &Backtest{
		Request: &BacktestRequest{
			Name:            "summary-run",
			Instrument:      "EURUSD",
			Strategy:        fake,
			TimeRange:       TimeRange{Start: Timestamp(1), End: Timestamp(3601), TF: H1},
			StartingBalance: MoneyFromFloat(10_000),
			RiskPct:         RateFromFloat(0.01),
			DefaultStopPips: pipsFromFloat(20),
		},
		State: &BacktestRun{},
		Result: &BacktestResult{
			Trades:    10,
			Wins:      6,
			Losses:    4,
			Balance:   MoneyFromFloat(10_250),
			NetPL:     MoneyFromFloat(250),
			ReturnPct: RateFromFloat(0.025),
			WinRate:   RateFromFloat(0.6),
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
	assert.InDelta(t, 1.0, s.RiskPct, 1e-9)
	assert.Equal(t, "", s.Stop) // Fake strategy returns no stop description
}
