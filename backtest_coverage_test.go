package trader

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBacktestReq_Success(t *testing.T) {
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

	req := newBacktestReq(rc)
	require.NotNil(t, req)
	assert.Equal(t, "run-1", req.Name)
	assert.Equal(t, "EURUSD", req.Instrument)
	assert.Equal(t, H1, req.TimeRange.TF)
	require.NotNil(t, req.Strategy)
	assert.Equal(t, "Fake", req.Strategy.Name())
}

func TestNewBacktestReq_InvalidInputs(t *testing.T) {
	t.Parallel()

	badDates := RunConfig{
		Data:     DataConfig{From: "bad-date", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: StrategyConfig{Kind: "fake"},
	}
	assert.Nil(t, newBacktestReq(badDates))

	badStrategy := RunConfig{
		Data:     DataConfig{From: "2026-01-01", To: "2026-01-10", Timeframe: "H1", Instrument: "EURUSD"},
		Strategy: StrategyConfig{Kind: "not-supported"},
	}
	assert.Nil(t, newBacktestReq(badStrategy))
}

func TestGetBacktests_SuccessAndDefaultsApplied(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Defaults: RunDefaults{
			StartingBalance: 12_500,
			RiskPct:         1.5,
			StopPips:        20,
			TakePips:        40,
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

	runs, err := GetBacktests(cfg)
	require.NoError(t, err)
	require.Len(t, runs, 2)

	for _, run := range runs {
		require.NotNil(t, run.BacktestRequest)
		require.NotNil(t, run.BacktestRun)
		assert.Equal(t, MoneyFromFloat(12_500), run.StartingBalance)
		assert.Equal(t, RateFromFloat(0.015), run.RiskPct)
		assert.Equal(t, pipsFromFloat(20), run.DefaultStopPips)
		assert.Equal(t, pipsFromFloat(40), run.DefaultTakePips)
		assert.NotEmpty(t, run.ID)
	}
}

func TestGetBacktests_ErrorCases(t *testing.T) {
	t.Parallel()

	_, err := GetBacktests(&Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve to exactly 1 run")

	cfg := &Config{
		Runs: []RunConfig{{
			Data:     DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-10"},
			Strategy: StrategyConfig{Kind: "unknown"},
		}},
	}
	_, err = GetBacktests(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create BacktestRequest")
}

func TestBuildBacktestResult(t *testing.T) {
	t.Parallel()

	var nilRun *Backtest
	assert.Nil(t, nilRun.BuildBacktestResult(&Account{}))

	run := &Backtest{
		BacktestRequest: &BacktestRequest{StartingBalance: MoneyFromFloat(10_000)},
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

	run.TimeRange = TimeRange{Start: Timestamp(100), End: Timestamp(200)}
	res := run.BuildBacktestResult(acct)
	require.NotNil(t, res)
	assert.Equal(t, acct.Balance, res.Balance)
	assert.Equal(t, acct.Equity, res.Equity)
	assert.Equal(t, 4, res.Trades)
	assert.Equal(t, 1, res.Wins)
	assert.Equal(t, 1, res.Losses)
	assert.Equal(t, Timestamp(100), res.Start)
	assert.Equal(t, Timestamp(200), res.End)
	assert.Equal(t, acct.Balance-run.StartingBalance, res.NetPL)
	assert.Equal(t, RateFromFloat(1.0/4.0), res.WinRate)
	assert.Equal(t, RateFromFloat(res.NetPL.Float64()/run.StartingBalance.Float64()), res.ReturnPct)
	assert.Same(t, res, run.BacktestResult)
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
		BacktestRequest: &BacktestRequest{
			Name:            "summary-run",
			Instrument:      "EURUSD",
			Strategy:        fake,
			TimeRange:       TimeRange{Start: Timestamp(1), End: Timestamp(3601), TF: H1},
			StartingBalance: MoneyFromFloat(10_000),
			RiskPct:         RateFromFloat(0.01),
			DefaultStopPips: pipsFromFloat(20),
		},
		BacktestResult: &BacktestResult{
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
	assert.Equal(t, int32(pipsFromFloat(20)), s.StopPips)
}

func TestConfigRunRequest_CurrentBehavior(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	_, err := cfg.runRequest(RunConfig{
		Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-01", To: "2026-01-10"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date range")

	r, err := cfg.runRequest(RunConfig{
		Data: DataConfig{Instrument: "EURUSD", Timeframe: "H1", From: "2026-01-10", To: "2026-01-01"},
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NotNil(t, r.BacktestRequest)
	assert.Equal(t, "EURUSD", r.Instrument)
	assert.True(t, r.TimeRange.Valid())
	assert.Equal(t, H1, r.TimeRange.TF)
}

func TestPrintBacktestAndNewBacktestReportSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	PrintBacktest(&buf, BacktestResult{})
	assert.Equal(t, "", buf.String())

	s := NewBacktestReportSummary(&BacktestResult{Trades: 3})
	assert.Equal(t, BacktestReportSummary{}, s)
}
