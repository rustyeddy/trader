package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
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

	acct := execution.NewAccount("acct", market.MoneyFromFloat(10_000))
	broker := execution.NewBroker("broker")
	broker.Account = acct

	tr := &engine.Trader{Broker: broker}
	ctx := context.Background()

	run := &Backtest{Request: &BacktestRequest{Instrument: "EURUSD"}}
	require.ErrorContains(t, run.runWithIterator(ctx, tr, nil), "nil candle iterator")

	itr := &fixedCandleIterator{candles: []market.CandleTime{{Candle: market.Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: market.Timestamp(1704067200)}}}
	require.ErrorContains(t, run.runWithIterator(ctx, tr, itr), "nil strategy")

	strat := &countingStrategy{}
	run.Request.Strategy = strat
	run.State = &BacktestRun{}

	itr = &fixedCandleIterator{candles: []market.CandleTime{{Candle: market.Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: market.Timestamp(1704067200)}}}
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
			TimeRange:       market.TimeRange{Start: market.Timestamp(1704067200), End: market.Timestamp(1704070800), TF: market.H1},
			StartingBalance: market.MoneyFromFloat(10_000),
		},
	}

	var nilTrader *engine.Trader
	require.ErrorContains(t, run.Execute(ctx, nilTrader), "nil trader")

	noAcct := &engine.Trader{Broker: execution.NewBroker("no-account")}
	require.ErrorContains(t, run.Execute(ctx, noAcct), "nil account")

	withAcctBroker := execution.NewBroker("with-account")
	withAcctBroker.Account = execution.NewAccount("acct", market.MoneyFromFloat(10_000))
	withAcct := &engine.Trader{Broker: withAcctBroker}
	require.ErrorContains(t, run.Execute(ctx, withAcct), "nil data manager")

	broker := execution.NewBroker("broker")
	broker.Account = withAcctBroker.Account

	ts := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	s := marketdata.NewStoreAt(t.TempDir())
	t.Cleanup(marketdata.SwapStore(s))
	cs, err := marketdata.NewMonthlyCandleSet("EURUSD", market.H1, market.FromTime(ts), market.PriceScale, market.SourceCandles)
	require.NoError(t, err)
	require.NoError(t, cs.AddCandle(market.FromTime(ts), market.Candle{
		Open:      market.Price(1100000),
		High:      market.Price(1102000),
		Low:       market.Price(1099000),
		Close:     market.Price(1101000),
		AvgSpread: market.Price(10),
		MaxSpread: market.Price(20),
		Ticks:     42,
	}))
	require.NoError(t, s.WriteCSV(cs))

	dm := marketdata.NewDataManager([]string{"EURUSD"}, ts, ts.Add(time.Hour))
	okTrader := &engine.Trader{Broker: broker, DataManager: dm}
	require.NoError(t, run.Execute(ctx, okTrader))
	require.NotNil(t, run.State)
	require.NotNil(t, run.Result)
	assert.Equal(t, 0, run.Result.Trades)
}
