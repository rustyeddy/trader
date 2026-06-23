package trader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	plan   *StrategyPlan
	resets int
	calls  int
}

func (s *countingStrategy) Name() string            { return "counting" }
func (s *countingStrategy) Reset()                  { s.resets++ }
func (s *countingStrategy) Ready() bool             { return true }
func (s *countingStrategy) StopDescription() string { return "" }
func (s *countingStrategy) Update(context.Context, *CandleTime, StrategyContext) *StrategyPlan {
	s.calls++
	return s.plan
}

type fixedCandleIterator struct {
	candles  []candleTime
	idx      int
	closeErr error
	err      error
}

func (it *fixedCandleIterator) Next() (CandleTime, bool) {
	if it.err != nil {
		return CandleTime{}, false
	}
	if it.idx >= len(it.candles) {
		return CandleTime{}, false
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
		tr.processEvent(context.Background(), &Event{Type: EventOrderFilled}),
		"no position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &Event{Type: EventPositionClosed}),
		"missing position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &Event{Type: EventPositionClosed, Lot: &Lot{TradeCommon: &TradeCommon{ID: NewULID()}}}),
		"missing trade")

	require.NoError(t,
		tr.processEvent(context.Background(), &Event{Type: EventOrderFilled, Lot: &Lot{TradeCommon: &TradeCommon{ID: NewULID()}}}))

	require.NoError(t,
		tr.processEvent(context.Background(), &Event{
			Type:  EventPositionClosed,
			Lot:   &Lot{TradeCommon: &TradeCommon{ID: NewULID()}},
			Trade: &Trade{TradeCommon: &TradeCommon{ID: NewULID()}},
		}))

	// Unsupported event types are intentionally non-fatal.
	require.NoError(t, tr.processEvent(context.Background(), &Event{Type: EventType(255)}))
}

func TestTraderStartBrokerEventHandler_ProcessesAndPropagatesError(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	evtQ := make(chan *Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed int64
	errCh, done := tr.startBrokerEventHandler(ctx, evtQ, &processed)

	evtQ <- &Event{Type: EventOrderFilled, Lot: &Lot{TradeCommon: &TradeCommon{ID: NewULID()}}}
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 200*time.Millisecond, 5*time.Millisecond)

	evtQ <- &Event{Type: EventOrderFilled} // triggers processEvent error

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
	require.NoError(t, tr.brokerEventError(errCh))

	errCh <- errors.New("boom")
	require.EqualError(t, tr.brokerEventError(errCh), "boom")

	b := NewBroker("idle")
	b.Account = NewAccount("acct", MoneyFromFloat(10_000))
	b.evtQ = make(chan *Event, 1)
	idle := &Trader{Broker: b}
	require.NoError(t, idle.waitForBrokerIdle(make(chan error, 1), 5*time.Millisecond))

	bad := make(chan error, 1)
	bad <- errors.New("from broker")
	require.EqualError(t, idle.waitForBrokerIdle(bad, 5*time.Millisecond), "from broker")

	b.evtQ <- &Event{Type: EventOrderFilled, Lot: &Lot{TradeCommon: &TradeCommon{ID: NewULID()}}}
	require.ErrorContains(t, idle.waitForBrokerIdle(make(chan error, 1), 5*time.Millisecond), "broker did not become idle")
}

func TestSnapshotLots_FiltersByState(t *testing.T) {
	t.Parallel()

	src := &LotBook{}
	src.Add(&Lot{TradeCommon: &TradeCommon{ID: "open"}, State: LotOpen})
	src.Add(&Lot{TradeCommon: &TradeCommon{ID: "open-req"}, State: LotOpenRequested})
	src.Add(&Lot{TradeCommon: &TradeCommon{ID: "close-req"}, State: LotCloseRequested})
	src.Add(&Lot{TradeCommon: &TradeCommon{ID: "closed"}, State: LotClosed})

	got := snapshotLots(src)
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

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	broker := NewBroker("broker")
	broker.Account = acct

	tr := &Trader{Broker: broker}
	ctx := context.Background()

	run := &Backtest{Request: &BacktestRequest{Instrument: "EURUSD"}}
	require.ErrorContains(t, tr.backTestWithIterator(ctx, run, nil), "nil candle iterator")

	itr := &fixedCandleIterator{candles: []candleTime{{Candle: Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: Timestamp(1704067200)}}}
	require.ErrorContains(t, tr.backTestWithIterator(ctx, run, itr), "nil strategy")

	strat := &countingStrategy{}
	run.Request.Strategy = strat
	run.State = &BacktestRun{}

	itr = &fixedCandleIterator{candles: []candleTime{{Candle: Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100000}, Timestamp: Timestamp(1704067200)}}}
	require.NoError(t, tr.backTestWithIterator(ctx, run, itr))
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
			TimeRange:       TimeRange{Start: Timestamp(1704067200), End: Timestamp(1704070800), TF: H1},
			StartingBalance: MoneyFromFloat(10_000),
		},
	}

	var nilTrader *Trader
	require.ErrorContains(t, nilTrader.Backtest(ctx, run), "nil trader")

	noAcct := &Trader{Broker: NewBroker("no-account")}
	require.ErrorContains(t, noAcct.Backtest(ctx, run), "nil account")

	withAcctBroker := NewBroker("with-account")
	withAcctBroker.Account = NewAccount("acct", MoneyFromFloat(10_000))
	withAcct := &Trader{Broker: withAcctBroker}
	require.ErrorContains(t, withAcct.Backtest(ctx, run), "nil data manager")

	broker := NewBroker("broker")
	broker.Account = withAcctBroker.Account

	ts := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	s := NewStoreAt(t.TempDir())
	t.Cleanup(SwapStore(s))
	cs, err := newMonthlyCandleSet("EURUSD", H1, FromTime(ts), PriceScale, SourceCandles)
	require.NoError(t, err)
	require.NoError(t, cs.AddCandle(FromTime(ts), Candle{
		Open:      Price(1100000),
		High:      Price(1102000),
		Low:       Price(1099000),
		Close:     Price(1101000),
		AvgSpread: Price(10),
		MaxSpread: Price(20),
		Ticks:     42,
	}))
	require.NoError(t, s.WriteCSV(cs))

	dm := NewDataManager([]string{"EURUSD"}, ts, ts.Add(time.Hour))
	okTrader := &Trader{Broker: broker, DataManager: dm}
	require.NoError(t, okTrader.Backtest(ctx, run))
	require.NotNil(t, run.State)
	require.NotNil(t, run.Result)
	assert.Equal(t, 0, run.Result.Trades)
}
