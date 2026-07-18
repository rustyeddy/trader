package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	sig    strategy.Signal
	resets int
	calls  int
}

func (s *countingStrategy) Name() string            { return "counting" }
func (s *countingStrategy) Reset()                  { s.resets++ }
func (s *countingStrategy) Ready() bool             { return true }
func (s *countingStrategy) StopDescription() string { return "" }
func (s *countingStrategy) Update(context.Context, *market.CandleTime, strategy.StrategyContext) strategy.Signal {
	s.calls++
	return s.sig
}

type fixedCandleIterator struct {
	candles  []market.CandleTime
	idx      int
	closeErr error
	err      error
}

func (it *fixedCandleIterator) Next() (market.CandleTime, bool) {
	if it.err != nil {
		return market.CandleTime{}, false
	}
	if it.idx >= len(it.candles) {
		return market.CandleTime{}, false
	}
	ct := it.candles[it.idx]
	it.idx++
	return ct, true
}

func (it *fixedCandleIterator) Err() error {
	return it.err
}

func (it *fixedCandleIterator) Close() error {
	return it.closeErr
}

func TestBackTestWithIterator_BasicPaths(t *testing.T) {
	t.Parallel()

	acct := execution.NewAccount("acct", types.MoneyFromFloat(10_000))
	broker := execution.NewBroker("broker")
	broker.Account = acct

	tr := &engine.Trader{Broker: broker}
	ctx := context.Background()

	run := &Backtest{Request: &BacktestRequest{Instrument: "EURUSD"}}
	require.ErrorContains(t, run.runWithIterator(ctx, tr, nil), "nil candle iterator")

	itr := &fixedCandleIterator{candles: []market.CandleTime{{Candle: market.Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: types.Timestamp(1704067200)}}}
	require.ErrorContains(t, run.runWithIterator(ctx, tr, itr), "nil strategy")

	strat := &countingStrategy{}
	run.Request.Strategy = strat
	run.State = &BacktestRun{}

	itr = &fixedCandleIterator{candles: []market.CandleTime{{Candle: market.Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: types.Timestamp(1704067200)}}}
	require.NoError(t, run.runWithIterator(ctx, tr, itr))
	assert.Equal(t, 1, strat.resets)
	assert.Equal(t, 1, strat.calls)
}

func TestTraderBacktest_GuardsAndSuccess(t *testing.T) {

	ctx := context.Background()
	strat := &countingStrategy{}
	run := &Backtest{
		Request: &BacktestRequest{
			Instrument:      "EURUSD",
			Strategy:        strat,
			TimeRange:       types.TimeRange{Start: types.Timestamp(1704067200), End: types.Timestamp(1704070800), TF: types.H1},
			StartingBalance: types.MoneyFromFloat(10_000),
		},
	}

	var nilTrader *engine.Trader
	require.ErrorContains(t, run.Execute(ctx, nilTrader), "nil trader")

	noAcct := &engine.Trader{Broker: execution.NewBroker("no-account")}
	require.ErrorContains(t, run.Execute(ctx, noAcct), "nil account")

	withAcctBroker := execution.NewBroker("with-account")
	withAcctBroker.Account = execution.NewAccount("acct", types.MoneyFromFloat(10_000))
	withAcct := &engine.Trader{Broker: withAcctBroker}
	require.ErrorContains(t, run.Execute(ctx, withAcct), "nil data manager")

	broker := execution.NewBroker("broker")
	broker.Account = withAcctBroker.Account

	ts := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	datamanager.UseTempDataDir(t)
	cs, err := datamanager.NewMonthlyCandleSet("EURUSD", types.H1, types.FromTime(ts), types.PriceScale, market.SourceCandles)
	require.NoError(t, err)
	require.NoError(t, cs.AddCandle(types.FromTime(ts), market.Candle{
		Open:      types.Price(1100000),
		High:      types.Price(1102000),
		Low:       types.Price(1099000),
		Close:     types.Price(1101000),
		AvgSpread: types.Price(10),
		MaxSpread: types.Price(20),
		Ticks:     42,
	}))
	datamanager.WriteCandleSet(t, cs)

	dm := datamanager.NewDataManager([]string{"EURUSD"}, ts, ts.Add(time.Hour))
	okTrader := &engine.Trader{Broker: broker, DataManager: dm}
	require.NoError(t, run.Execute(ctx, okTrader))
	require.NotNil(t, run.State)
	require.NotNil(t, run.Result)
	assert.Equal(t, 0, run.Result.Trades)
}
