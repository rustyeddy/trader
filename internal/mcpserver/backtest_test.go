package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/report"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
	"github.com/rustyeddy/trader/num"
)

// fakeBacktests runs nothing: Run reports two stages and returns rep (or
// err); Result returns rep (or err).
type fakeBacktests struct {
	rep    report.BacktestReport
	err    error
	gotReq BacktestInput
}

func (f *fakeBacktests) Run(_ context.Context, req BacktestInput, progress func(string)) (report.BacktestReport, error) {
	f.gotReq = req
	if f.err != nil {
		return report.BacktestReport{}, f.err
	}
	progress("preparing market data")
	progress("running backtest")
	return f.rep, nil
}

func (f *fakeBacktests) Result(context.Context, string) (report.BacktestReport, error) {
	return f.rep, f.err
}

// sampleReport is a minimal report that encodes like a real one: every
// money value carries a currency.
func sampleReport() report.BacktestReport {
	currency := num.MustParseCurrency("USD")
	usd := num.MustParseMoney("0", currency)
	trade := report.TradeReport{RealizedPnL: usd, Costs: usd}
	return report.BacktestReport{
		Run:         report.RunInfo{RunID: "run_01HKK5WY00MH56VK6QJDVTCQ86", StrategyName: "buy-and-hold", ConfigDigest: "sha256:abc"},
		Performance: report.Performance{StartingCapital: usd, FinalEquity: usd},
		TradeStats:  report.TradeStats{GrossPnL: usd, ClosedTradeCosts: usd, AccountFees: usd, NetPnL: usd},
		Account: report.AccountReport{
			Currency: currency, Equity: usd, BuyingPower: usd, MarginUsed: usd, MarginAvailable: usd,
			RealizedPnL: usd, UnrealizedPnL: usd, Fees: usd, Financing: usd,
		},
		ClosedTrades: []report.TradeReport{trade, trade},
		OpenTrades:   []report.TradeReport{trade},
		EquityCurve:  []report.EquityPointReport{{Equity: usd}},
	}
}

func TestRunBacktestTool(t *testing.T) {
	fake := &fakeBacktests{rep: sampleReport()}
	session := connect(t, Deps{Backtests: fake})

	var out RunBacktestOutput
	result := call(t, session, "trader_run_backtest", map[string]any{
		"symbols": []string{"EURUSD"}, "from": "2024-01-01", "to": "2024-02-01", "adverse_distance": "0.01",
		"strategy": map[string]any{"name": "ema-cross", "fast_period": 3},
	}, &out)
	require.False(t, result.IsError, errorText(result))
	assert.Equal(t, "run_01HKK5WY00MH56VK6QJDVTCQ86", out.RunID)
	assert.Equal(t, "sha256:abc", out.ConfigDigest)
	assert.Contains(t, out.Summary, "run")
	assert.Contains(t, out.Summary, "performance")
	assert.NotContains(t, out.Summary, "equity_curve")
	assert.NotContains(t, out.Summary, "closed_trades")
	assert.Equal(t, float64(2), out.Summary["closed_trade_count"])
	assert.Equal(t, float64(1), out.Summary["open_trade_count"])
	three := 3
	assert.Equal(t, BacktestStrategy{Name: "ema-cross", FastPeriod: &three}, fake.gotReq.Strategy, "the request reaches the capability as sent")
	assert.Nil(t, fake.gotReq.Strategy.SlowPeriod, "an omitted period stays omitted")
}

func TestBacktestResultTool(t *testing.T) {
	session := connect(t, Deps{Backtests: &fakeBacktests{rep: sampleReport()}})
	var out BacktestResultOutput
	result := call(t, session, "trader_backtest_result", map[string]any{"run_id": "run_01HKK5WY00MH56VK6QJDVTCQ86"}, &out)
	require.False(t, result.IsError)
	assert.Equal(t, "run_01HKK5WY00MH56VK6QJDVTCQ86", out.RunID)

	want, err := json.Marshal(sampleReport())
	require.NoError(t, err)
	got, err := json.Marshal(out.Report)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got), "the full report, as the report renders to JSON")
}

func TestBacktestTools_Errors(t *testing.T) {
	runArgs := map[string]any{"symbols": []string{"EURUSD"}, "from": "2024-01-01", "to": "2024-02-01", "adverse_distance": "0.01"}

	t.Run("unavailable", func(t *testing.T) {
		session := connect(t, Deps{})
		for tool, args := range map[string]map[string]any{"trader_run_backtest": runArgs, "trader_backtest_result": {"run_id": "x"}} {
			result := call(t, session, tool, args, nil)
			require.True(t, result.IsError, tool)
			assert.Contains(t, errorText(result), ErrBacktestsUnavailable.Error())
		}
	})
	for name, tc := range map[string]struct {
		err      error
		want     string
		withheld bool
	}{
		"invalid input":  {fmt.Errorf("%w: strategy.name %q is not registered", ErrInvalidBacktest, "typo"), `strategy.name "typo" is not registered`, false},
		"data not ready": {fmt.Errorf("%w: EURUSD H1 2024-01 is missing", ErrBacktestDataNotReady), "EURUSD H1 2024-01 is missing", false},
		"not found":      {fmt.Errorf("%w: run_x", svcbacktest.ErrRunNotFound), "no stored run with this id", false},
		"schema":         {fmt.Errorf("%w: file has version 99", svcbacktest.ErrSnapshotVersionMismatch), "version 99", false},
		"internal":       {errors.New("open " + secretPath + ": permission denied"), "backtest failed; see the trader-mcp server log", true},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			session := connect(t, Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil)), Backtests: &fakeBacktests{err: tc.err}})
			result := call(t, session, "trader_run_backtest", runArgs, nil)
			require.True(t, result.IsError)
			assert.Contains(t, errorText(result), tc.want)
			assert.NotContains(t, errorText(result), "/srv/secret")
			if tc.withheld {
				assert.Contains(t, logs.String(), secretPath, "the full cause is logged")
			}
		})
	}
}

func TestRunBacktestTool_ProgressPerStage(t *testing.T) {
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Deps{Backtests: &fakeBacktests{rep: sampleReport()}}).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	var mu sync.Mutex
	var stages []string
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			stages = append(stages, fmt.Sprintf("%s %v/%v", req.Params.Message, req.Params.Progress, req.Params.Total))
		},
	})
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	params := &mcp.CallToolParams{Name: "trader_run_backtest", Arguments: map[string]any{
		"symbols": []string{"EURUSD"}, "from": "2024-01-01", "to": "2024-02-01", "adverse_distance": "0.01",
	}}
	params.SetProgressToken("bt-1")
	_, err = session.CallTool(ctx, params)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(stages) == 3
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"preparing market data 1/3", "running backtest 2/3", "saved 3/3"}, stages)
}

// externalParams is what backtestcfg records for a CLI --strategy-exec run.
const externalParams = `{"mode":"external","strategy_name":"guest","strategy_version":"v1","protocol_version":"1","transport":"unix",` +
	`"exec":"/home/op/bin/guest","exec_digest":"sha256:e1","args":["--token","s3cret"],"config":"/home/op/guest.yaml","config_digest":"sha256:c1"}`

func TestBacktestTools_RedactExternalLaunchDetails(t *testing.T) {
	rep := sampleReport()
	rep.Run.StrategyParameters = json.RawMessage(externalParams)
	session := connect(t, Deps{Backtests: &fakeBacktests{rep: rep}})

	var result BacktestResultOutput
	require.False(t, call(t, session, "trader_backtest_result", map[string]any{"run_id": rep.Run.RunID}, &result).IsError)
	var run RunBacktestOutput
	require.False(t, call(t, session, "trader_run_backtest", map[string]any{
		"symbols": []string{"EURUSD"}, "from": "2024-01-01", "to": "2024-02-01", "adverse_distance": "0.01",
	}, &run).IsError)

	for name, params := range map[string]any{
		"result":  result.Report["run"].(map[string]any)["strategy_parameters"],
		"summary": run.Summary["run"].(map[string]any)["strategy_parameters"],
	} {
		p := params.(map[string]any)
		for _, local := range []string{"exec", "config", "args"} {
			assert.NotContains(t, p, local, "%s: %s is machine-local", name, local)
		}
		assert.Equal(t, "sha256:e1", p["exec_digest"], "%s: the content digests remain", name)
		assert.Equal(t, "sha256:c1", p["config_digest"], name)
		assert.Equal(t, "guest", p["strategy_name"], name)
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "/home/op")
	assert.NotContains(t, string(encoded), "s3cret")
}

func TestWithoutLaunchDetails_LeavesOtherStrategiesAlone(t *testing.T) {
	for _, params := range []string{``, `{"fast_period":3,"slow_period":5}`, `not json`} {
		run := report.RunInfo{StrategyParameters: json.RawMessage(params)}
		assert.Equal(t, run, withoutLaunchDetails(run), params)
	}
}
