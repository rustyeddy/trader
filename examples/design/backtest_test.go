package main

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptStrategy returns a fixed order per bar index (nil means no order) and
// records the bars it was shown.
type scriptStrategy struct {
	orders []*order
	seen   []bar
}

func (s *scriptStrategy) name() string { return "script" }

func (s *scriptStrategy) init(map[string]any) error { return nil }

func (s *scriptStrategy) onBar(_ context.Context, b bar) *order {
	i := len(s.seen)
	s.seen = append(s.seen, b)
	if i < len(s.orders) && s.orders[i] != nil {
		s.orders[i].Time = b.Time // a strategy stamps its orders with the bar
		return s.orders[i]
	}
	return nil
}

// openOrder is a valid open-long order for EURUSD.
func openOrder() *order {
	return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: 108900}
}

// eurBars is closes for EURUSD, the instrument the test account can size.
func eurBars(vals ...float64) []bar {
	bars := closes(vals...)
	for i := range bars {
		bars[i].instrument = "EURUSD"
	}
	return bars
}

// runTrader runs the bars through a trader built on the strategy.
func runTrader(ctx context.Context, tr *trader, strat strategy, bars iter.Seq2[bar, error]) error {
	tr.strategy = strat
	return (&backtest{}).run(ctx, bars, tr)
}

// opens returns n distinct valid open orders, one per bar.
func opens(n int) []*order {
	orders := make([]*order, n)
	for i := range orders {
		orders[i] = openOrder()
	}
	return orders
}

func seq(bars []bar, errAt int, err error) iter.Seq2[bar, error] {
	return func(yield func(bar, error) bool) {
		for i, b := range bars {
			if i == errAt {
				yield(bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func TestBacktestRun_AcceptedRequests(t *testing.T) {
	captureLogs(t)
	bars := eurBars(1, 2, 3)
	o1 := openOrder()
	o3 := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}
	strat := &scriptStrategy{orders: []*order{o1, nil, o3}}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(bars, -1, nil))

	require.NoError(t, err)
	assert.Equal(t, bars, strat.seen, "every bar reaches the strategy")
	assert.Equal(t, stateFilled, o1.state, "filled on the bar after it was made")
	assert.Equal(t, stateSubmitted, o3.state, "made on the last bar, so still waiting")
	assert.Equal(t, "a", o1.accountID)
}

func TestBacktestRun_NoOrderNothingSubmitted(t *testing.T) {
	strat := &scriptStrategy{} // returns nil on every bar
	tr := newTestTrader()

	require.NoError(t, runTrader(context.Background(), tr, strat, seq(eurBars(1, 2, 3), -1, nil)))
	assert.Len(t, strat.seen, 3)
	assert.Zero(t, tr.broker.seq, "nothing reached the broker")
}

func TestBacktestRun_NoBarsNoCalls(t *testing.T) {
	strat := &scriptStrategy{}

	require.NoError(t, runTrader(context.Background(), newTestTrader(), strat, seq(nil, -1, nil)))
	assert.Empty(t, strat.seen)
}

func TestBacktestRun_RecordsLifecycleInHistory(t *testing.T) {
	captureLogs(t)
	o := openOrder()
	strat := &scriptStrategy{orders: []*order{nil, o}}
	bars := eurBars(1, 2)

	require.NoError(t, runTrader(context.Background(), newTestTrader(), strat, seq(bars, -1, nil)))

	require.Len(t, o.history, 3)
	for _, e := range o.history {
		assert.Equal(t, bars[1].Time, e.time, "history carries the bar time, not the wall clock")
	}
	assert.Equal(t, []orderState{stateRequested, stateSized, stateSubmitted},
		[]orderState{o.history[0].state, o.history[1].state, o.history[2].state})
}

func TestBacktestRun_FatalHandlerErrorStopsRun(t *testing.T) {
	captureLogs(t)
	strat := &scriptStrategy{orders: opens(4)}
	tr := &trader{broker: &broker{id: "t"}, account: newAccount("ghost", usd(1000))} // the broker has no such account

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1, 2, 3, 4), -1, nil))

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Len(t, strat.seen, 1, "no bars processed after the failure")
}

func TestBacktestRun_FatalHandlerErrorHasInstrumentAndTimeContext(t *testing.T) {
	captureLogs(t)
	strat := &scriptStrategy{orders: []*order{nil, openOrder()}}
	tr := &trader{broker: &broker{id: "t"}, account: newAccount("ghost", usd(1000))}

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1, 2, 3), -1, nil))

	require.ErrorIs(t, err, errUnknownAccount, "cause preserved")
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the second bar")
}

func TestBacktestRun_FatalErrorIsNotLoggedAsRejection(t *testing.T) {
	logs := captureLogs(t)
	o := openOrder()
	tr := &trader{broker: &broker{id: "t"}, account: newAccount("ghost", usd(1000))}

	err := runTrader(context.Background(), tr, &scriptStrategy{orders: []*order{o}}, seq(eurBars(1, 2), -1, nil))

	require.Error(t, err)
	assert.NotContains(t, logs.String(), "order rejected")
	assert.Equal(t, stateRequested, o.state, "a fatal error is not recorded as a rejection")
}

func TestBacktestRun_DataErrorStopsRun(t *testing.T) {
	boom := errors.New("bad data")
	strat := &scriptStrategy{}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(eurBars(1, 2, 3), 2, boom))

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "bars before the error are processed, none after")
}

func TestBacktestRun_DataErrorHasContext(t *testing.T) {
	boom := errors.New("bad data")

	// error on the first bar: nothing processed yet
	err := runTrader(context.Background(), newTestTrader(), &scriptStrategy{}, seq(eurBars(1, 2), 0, boom))
	require.ErrorIs(t, err, boom)

	// error after bars were processed: mentions the last good bar
	err = runTrader(context.Background(), newTestTrader(), &scriptStrategy{}, seq(eurBars(1, 2, 3), 2, boom))
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "last successfully processed bar")
}

// cancelStrategy cancels the context as it sees its first bar.
type cancelStrategy struct {
	scriptStrategy
	cancel context.CancelFunc
}

func (s *cancelStrategy) onBar(ctx context.Context, b bar) *order {
	s.cancel()
	return s.scriptStrategy.onBar(ctx, b)
}

func TestBacktestRun_ContextCancelled(t *testing.T) {
	md := &marketData{data: eurBars(1, 2, 3, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strat := &cancelStrategy{cancel: cancel}

	err := runTrader(ctx, newTestTrader(), strat, md.bars(ctx))

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, strat.seen, 1)
}

// captureLogs routes the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestBacktestRun_ExpectedRejectionIsRecordedLoggedAndRunContinues(t *testing.T) {
	logs := captureLogs(t)
	o := openOrder()
	strat := &scriptStrategy{orders: []*order{o}}

	err := runTrader(context.Background(), newTestTraderWithBalance(0), strat, seq(eurBars(1, 2, 3), -1, nil))

	require.NoError(t, err, "a margin rejection must not stop the run")
	assert.Len(t, strat.seen, 3, "bars after the rejection are still processed")

	assert.Equal(t, stateRejected, o.state)
	require.Len(t, o.history, 2)
	assert.Equal(t, stateRequested, o.history[0].state)
	assert.Equal(t, stateRejected, o.history[1].state)
	assert.Contains(t, o.history[1].reason, "insufficient margin")
	assert.Equal(t, closes(1)[0].Time, o.history[1].time, "stamped with the bar time")

	out := logs.String()
	assert.Contains(t, out, "to=rejected", "the state change is logged too")
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "order rejected")
	assert.Contains(t, out, "insufficient margin available")
	assert.Contains(t, out, "time=2010-01-04")
	assert.Contains(t, out, "instrument=EURUSD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
}

func TestBacktestRun_OrderTooSmallIsARejectionToo(t *testing.T) {
	captureLogs(t)
	// A balance of 1 buys less than one unit: a rejection, not a failure.
	o := openOrder()

	err := runTrader(context.Background(), newTestTraderWithBalance(usd(1)), &scriptStrategy{orders: []*order{o, nil}}, seq(eurBars(1, 2), -1, nil))

	require.NoError(t, err)
	assert.Equal(t, stateRejected, o.state)
}

func TestBacktestRun_InvalidOrderStopsRunBeforeSizing(t *testing.T) {
	bad := &order{Time: testStart, instrument: "EURUSD", action: open, price: eurusd1_10, stopLoss: 108900} // open without a direction
	strat := &scriptStrategy{orders: []*order{nil, bad, openOrder()}}
	tr := newTestTrader()

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1, 2, 3, 4), -1, nil))

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the offending bar")
	assert.Len(t, strat.seen, 2, "no bars processed after the invalid order")
	assert.Empty(t, bad.history, "an invalid order is never recorded as requested")
	assert.Zero(t, tr.broker.seq)
}

func TestBacktestRun_OpenWithoutAStopStopsRunBeforeSizing(t *testing.T) {
	// The strategy, not the account, must set the stop.
	noStop := &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10}
	strat := &scriptStrategy{orders: []*order{nil, noStop, openOrder()}}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(eurBars(1, 2, 3, 4), -1, nil))

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "stop loss")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the offending bar")
	assert.Len(t, strat.seen, 2, "no bars processed after the invalid order")
	assert.Empty(t, noStop.history)
}

// rejectFirstOpen wraps a strategy, records every order it emits with the bar
// index, and starves the account of margin until the bar after its first open,
// so that open is rejected and every later one is accepted.
type rejectFirstOpen struct {
	strategy
	act  *account
	cash money
	bars int
	got  []*order
	at   []int
}

func (s *rejectFirstOpen) onBar(ctx context.Context, b bar) *order {
	if len(s.got) > 0 {
		s.act.balance = s.cash // margin becomes available on the bar after the open
	}
	o := s.strategy.onBar(ctx, b)
	if o != nil {
		s.got = append(s.got, o)
		s.at = append(s.at, s.bars)
	}
	s.bars++
	return o
}

func actions(orders []*order) (as []action) {
	for _, o := range orders {
		as = append(as, o.action)
	}
	return as
}

func newRejectFirstOpen(t *testing.T) (*trader, *rejectFirstOpen) {
	t.Helper()
	tr := newTestTrader()
	// These price series swing far enough to wipe out the account; the tests
	// are about missed entries, not liquidation.
	tr.broker.margin = func(*account) (marginStatus, error) { return marginHealthy, nil }
	s := &rejectFirstOpen{strategy: newTestEmax(t, 2, 4), act: tr.account, cash: tr.account.balance}
	tr.account.balance = 0
	return tr, s
}

// §2 critical regression: a rejected entry must not become a late fill.
func TestMissedEntry_RejectedOpenIsNotRetried(t *testing.T) {
	captureLogs(t)
	tr, s := newRejectFirstOpen(t)

	// Bullish from the first valid bar (3) and bullish for many bars after,
	// while margin "becomes available".
	err := runTrader(context.Background(), tr, s, seq(eurBars(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12), -1, nil))

	require.NoError(t, err)
	require.Equal(t, []action{open}, actions(s.got), "the strategy asked once and never again")
	assert.Equal(t, stateRejected, s.got[0].state)
	assert.Equal(t, []int{3}, s.at, "the one entry opportunity, bar 3")
}

// §2: a rejected entry must not suppress re-entry forever.
func TestMissedEntry_FreshSignalAfterRejectionIsAllowed(t *testing.T) {
	captureLogs(t)
	tr, s := newRejectFirstOpen(t)

	// Rising (open long at 3, rejected), a drop (close at 8, open short at 9),
	// then a rise again (close at 12, open long at 13).
	err := runTrader(context.Background(), tr, s, seq(eurBars(1, 2, 3, 4, 5, 6, 7, 8, 4, 2, 1, 0.5, 6, 9, 12), -1, nil))

	require.NoError(t, err)
	require.Equal(t, []action{open, close, open, close, open}, actions(s.got))
	assert.Equal(t, []int{3, 8, 9, 12, 13}, s.at)
	assert.Equal(t, stateRejected, s.got[0].state, "first opportunity was rejected")
	assert.Equal(t, short, s.got[2].direction)
	assert.Equal(t, stateRejected, s.got[1].state, "the close has no position: its open was rejected")
	assert.Contains(t, s.got[1].history[len(s.got[1].history)-1].reason, "no position")
	assert.Equal(t, stateFilled, s.got[2].state, "the fresh opportunity is accepted")
	assert.Equal(t, stateFilled, s.got[3].state)
	assert.Equal(t, stateFilled, s.got[4].state)
	require.Contains(t, tr.account.positions, "EURUSD")
	assert.Equal(t, long, tr.account.positions["EURUSD"].direction)
}
