package trader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	plan   *strategy.StrategyPlan
	resets int
	calls  int
}

func (s *countingStrategy) Name() string            { return "counting" }
func (s *countingStrategy) Reset()                  { s.resets++ }
func (s *countingStrategy) Ready() bool             { return true }
func (s *countingStrategy) StopDescription() string { return "" }
func (s *countingStrategy) Update(context.Context, *market.CandleTime, strategy.StrategyContext) *strategy.StrategyPlan {
	s.calls++
	return s.plan
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

func TestTraderProcessEventValidation(t *testing.T) {
	t.Parallel()

	tr := &Trader{}

	require.ErrorContains(t, tr.processEvent(context.Background(), nil), "nil broker event")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventOrderFilled}),
		"no position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventPositionClosed}),
		"missing position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventPositionClosed, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}}}),
		"missing trade")

	require.NoError(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}}}))

	require.NoError(t,
		tr.processEvent(context.Background(), &execution.Event{
			Type:  execution.EventPositionClosed,
			Lot:   &execution.Lot{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}},
			Trade: &execution.Trade{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}},
		}))

	// Unsupported event types are intentionally non-fatal.
	require.NoError(t, tr.processEvent(context.Background(), &execution.Event{Type: execution.EventType(255)}))
}

func TestTraderStartBrokerEventHandler_ProcessesAndPropagatesError(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	evtQ := make(chan *execution.Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed int64
	errCh, done := tr.StartBrokerEventHandler(ctx, evtQ, &processed)

	evtQ <- &execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}}}
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 200*time.Millisecond, 5*time.Millisecond)

	evtQ <- &execution.Event{Type: execution.EventOrderFilled} // triggers processEvent error

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "no position")
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected handler error")
	}

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected handler goroutine to stop")
	}
}

func TestTraderBrokerEventErrorAndWaitForBrokerIdle(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	errCh := make(chan error, 1)
	require.NoError(t, tr.BrokerEventError(errCh))

	errCh <- errors.New("boom")
	require.EqualError(t, tr.BrokerEventError(errCh), "boom")

	b := execution.NewBroker("idle")
	b.Account = execution.NewAccount("acct", market.MoneyFromFloat(10_000))
	idle := &Trader{Broker: b}
	require.NoError(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond))

	bad := make(chan error, 1)
	bad <- errors.New("from broker")
	require.EqualError(t, idle.WaitForBrokerIdle(bad, 5*time.Millisecond), "from broker")

	require.True(t, b.EnqueueEvent(&execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: market.NewULID()}}}))
	require.ErrorContains(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond), "broker did not become idle")
}

func TestSnapshotLots_FiltersByState(t *testing.T) {
	t.Parallel()

	src := &execution.LotBook{}
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "open"}, State: execution.LotOpen})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "open-req"}, State: execution.LotOpenRequested})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "close-req"}, State: execution.LotCloseRequested})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "closed"}, State: execution.LotClosed})

	got := SnapshotLots(src)
	require.NotNil(t, got)
	lots := got.All()
	require.Len(t, lots, 3)
	assert.Contains(t, lots, "open")
	assert.Contains(t, lots, "open-req")
	assert.Contains(t, lots, "close-req")
	assert.NotContains(t, lots, "closed")
}

func TestBackTestWithIterator_BasicPaths(t *testing.T) {
	t.Parallel()

	acct := execution.NewAccount("acct", market.MoneyFromFloat(10_000))
	broker := execution.NewBroker("broker")
	broker.Account = acct

	tr := &Trader{Broker: broker}
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

	var nilTrader *Trader
	require.ErrorContains(t, run.Execute(ctx, nilTrader), "nil trader")

	noAcct := &Trader{Broker: execution.NewBroker("no-account")}
	require.ErrorContains(t, run.Execute(ctx, noAcct), "nil account")

	withAcctBroker := execution.NewBroker("with-account")
	withAcctBroker.Account = execution.NewAccount("acct", market.MoneyFromFloat(10_000))
	withAcct := &Trader{Broker: withAcctBroker}
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
	okTrader := &Trader{Broker: broker, DataManager: dm}
	require.NoError(t, run.Execute(ctx, okTrader))
	require.NotNil(t, run.State)
	require.NotNil(t, run.Result)
	assert.Equal(t, 0, run.Result.Trades)
}
