package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBroker_Sim(t *testing.T) {
	br, ok := getBroker("sim")
	require.True(t, ok)
	assert.Equal(t, "sim", br.id)

	act, ok := br.getAccount("sim123")
	require.True(t, ok)
	assert.Equal(t, money(10000), act.balance)
}

func TestGetBroker_UnknownIsCreatedAndReused(t *testing.T) {
	t.Cleanup(func() { delete(brokers, "test-broker") })

	br, ok := getBroker("test-broker")
	require.True(t, ok)
	assert.Equal(t, "test-broker", br.id)
	assert.NotNil(t, br.accounts)

	again, ok := getBroker("test-broker")
	require.True(t, ok)
	assert.Same(t, br, again)
}

func TestBroker_AddAccountInitializesNilMap(t *testing.T) {
	b := &broker{id: "b"} // accounts map is nil
	act := b.addAccount("a1", 500)

	require.NotNil(t, act)
	assert.Equal(t, "a1", act.id)
	assert.Equal(t, money(500), act.balance)

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, act, got)
}

func TestBroker_AddAccountReplacesSameID(t *testing.T) {
	b := &broker{id: "b"}
	b.addAccount("a1", 500)
	second := b.addAccount("a1", 900)

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, second, got)
	assert.Equal(t, money(900), got.balance)
}

func TestBroker_GetAccountMissing(t *testing.T) {
	b := &broker{id: "b"}
	act, ok := b.getAccount("nope")
	assert.False(t, ok)
	assert.Nil(t, act)
}

func TestNewAccount(t *testing.T) {
	a := newAccount("x", 123)
	assert.Equal(t, "x", a.id)
	assert.Equal(t, money(123), a.balance)
}

func TestBroker_SubmitMarksSubmittedAssignsIDAndLogs(t *testing.T) {
	logs := captureLogs(t)
	ts := time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	o := &order{
		accountID:  "acct",
		instrument: "EUR/USD",
		direction:  long,
		action:     open,
		orderType:  marketOrder,
		units:      1000,
		Time:       ts,
	}

	require.NoError(t, b.submit(context.Background(), o))

	assert.Equal(t, "sim-1", o.id)
	assert.Equal(t, stateSubmitted, o.state)
	require.Len(t, o.history, 1)
	assert.Equal(t, stateSubmitted, o.history[0].state)
	assert.Equal(t, ts, o.history[0].time, "the broker stamps with the order's bar time")
	out := logs.String()
	assert.Contains(t, out, "to=submitted", "the state change is logged")
	assert.Contains(t, out, "order submitted")
	assert.Contains(t, out, "broker=sim")
	assert.Contains(t, out, "account=acct")
	assert.Contains(t, out, "order=sim-1")
	assert.Contains(t, out, "instrument=EUR/USD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
	assert.Contains(t, out, "type=market")
	assert.Contains(t, out, "units=1000")
}

func TestBroker_SubmitIDsAreSequentialAndKept(t *testing.T) {
	captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	ctx := context.Background()

	o1, o2 := &order{Time: testStart, accountID: "acct", action: open}, &order{Time: testStart, accountID: "acct", action: close}
	require.NoError(t, b.submit(ctx, o1))
	require.NoError(t, b.submit(ctx, o2))
	assert.Equal(t, "sim-1", o1.id)
	assert.Equal(t, "sim-2", o2.id)

	pre := &order{Time: testStart, id: "mine", accountID: "acct", action: open}
	require.NoError(t, b.submit(ctx, pre))
	assert.Equal(t, "mine", pre.id, "an existing id is not overwritten")
}

func TestBroker_SubmitUnknownAccount(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	o := &order{Time: testStart, accountID: "ghost", action: open}

	err := b.submit(context.Background(), o)

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.id)
	assert.Empty(t, o.history)
	assert.NotContains(t, logs.String(), "order submitted")
}

func TestBroker_SubmitCancelledContext(t *testing.T) {
	captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := &order{Time: testStart, accountID: "acct", action: open}

	require.ErrorIs(t, b.submit(ctx, o), context.Canceled)
	assert.Empty(t, o.id)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history)
}

func TestBroker_SubmitOrderWithoutAnAccountIsRejectedAsUnknown(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	o := &order{Time: testStart, action: open} // the trader never set an account

	err := b.submit(context.Background(), o)

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Contains(t, err.Error(), "no account")
	assert.Empty(t, o.id)
	assert.Empty(t, o.history)
	assert.NotContains(t, logs.String(), "order submitted")
}

func TestBroker_SubmitUsesTheOrdersAccountForTheLog(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("one", 1000)
	b.addAccount("two", 1000)

	require.NoError(t, b.submit(context.Background(), &order{Time: testStart, accountID: "two", action: open}))

	assert.Contains(t, logs.String(), "account=two")
	assert.NotContains(t, logs.String(), "account=one")
}

// fillBar is a EURUSD bar on day d (0 = testStart) opening at o.
func fillBar(d int, o price) bar {
	return bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, d), ohlc: ohlc{o: o, c: o + 100}}
}

// submitted is an order the broker has accepted on day 0.
func submittedOrder(t *testing.T, br *broker, a action, d direction, units int64) *order {
	t.Helper()
	o := &order{
		accountID: "a", instrument: "EURUSD", direction: d, action: a, orderType: marketOrder,
		units: units, price: eurusd1_10, stopLoss: 108900, Time: testStart,
	}
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func newFillBroker() (*broker, *account) {
	br := &broker{id: "t"}
	return br, br.addAccount("a", 10000)
}

func TestBroker_SubmitQueuesTheOrderOnTheAccount(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()

	o := submittedOrder(t, br, open, long, 100)

	assert.Equal(t, []*order{o}, act.orders["EURUSD"])
}

func TestAccount_OnBar_MarketOrderFillsAtTheNextBarsOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submittedOrder(t, br, open, long, 100)
	ctx := context.Background()

	require.NoError(t, act.onBar(ctx, fillBar(0, 111000)))
	assert.Equal(t, stateSubmitted, o.state, "never fills on the bar that produced it")
	assert.Empty(t, act.positions)

	require.NoError(t, act.onBar(ctx, fillBar(1, 111500)))
	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, price(111500), o.fillPrice, "the open of the next bar")
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.Time, "stamped with the fill bar")
	assert.Equal(t, stateFilled, o.history[len(o.history)-1].state)
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.history[len(o.history)-1].time)
	assert.Equal(t, &position{instrument: "EURUSD", direction: long, units: 100, entryPrice: 111500}, act.positions["EURUSD"])
	assert.Empty(t, act.orders["EURUSD"], "a filled order is no longer waiting")
}

func TestAccount_OnBar_IgnoresOtherInstrumentsAndUnsupportedTypes(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	market := submittedOrder(t, br, open, long, 100)
	limit := &order{accountID: "a", instrument: "EURUSD", direction: long, action: open, orderType: limitOrder, units: 5, Time: testStart}
	require.NoError(t, br.submit(context.Background(), limit))

	other := fillBar(1, 111500)
	other.instrument = "GBPUSD"
	require.NoError(t, act.onBar(context.Background(), other))
	assert.Equal(t, stateSubmitted, market.state, "a bar for another instrument does not fill it")

	require.NoError(t, act.onBar(context.Background(), fillBar(1, 111500)))
	assert.Equal(t, stateFilled, market.state)
	assert.Equal(t, stateSubmitted, limit.state, "limit orders do not execute yet")
	assert.Equal(t, []*order{limit}, act.orders["EURUSD"])
}

func TestAccount_OnBar_CloseTakesThePositionsSizeAndRemovesIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	open1 := submittedOrder(t, br, open, short, 250)
	require.NoError(t, act.onBar(ctx, fillBar(1, 111000)))
	require.Equal(t, stateFilled, open1.state)
	assert.Equal(t, price(111000), act.positions["EURUSD"].entryPrice, "the position remembers its fill price")

	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))
	require.NoError(t, act.onBar(ctx, fillBar(2, 112000)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, price(111000), open1.fillPrice)
	assert.Equal(t, int64(250), cl.units, "the order records what was closed")
	assert.Equal(t, price(112000), cl.fillPrice)
	assert.Empty(t, act.positions)
}

func TestAccount_OnBar_CloseWithoutAPositionIsRejectedNotFatal(t *testing.T) {
	logs := captureLogs(t)
	br, act := newFillBroker()
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(context.Background(), cl))

	require.NoError(t, act.onBar(context.Background(), fillBar(1, 111000)))

	assert.Equal(t, stateRejected, cl.state)
	assert.Contains(t, cl.history[len(cl.history)-1].reason, "no position to close")
	assert.Zero(t, cl.fillPrice)
	assert.Contains(t, logs.String(), "order rejected")
	assert.Empty(t, act.orders["EURUSD"], "a rejected order is no longer waiting")
}

func TestAccount_OnBar_OpenOverAnExistingPositionIsFatal(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10}
	o := submittedOrder(t, br, open, long, 100)

	err := act.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.False(t, isRejection(err))
	assert.Equal(t, stateSubmitted, o.state)
}

func TestAccount_ApplyFill_AddAndReduceAreUnsupported(t *testing.T) {
	act := newAccount("a", 1000)
	for _, a := range []action{add, reduce} {
		require.ErrorIs(t, act.applyFill(&order{instrument: "EURUSD", action: a}), errUnsupportedAction, a.String())
	}
}

func TestBroker_FillOrder_UnknownAccount(t *testing.T) {
	br := &broker{id: "t"}
	err := br.fillOrder(&order{id: "t-1", accountID: "ghost", instrument: "EURUSD", action: open}, 1)
	require.ErrorIs(t, err, errUnknownAccount)
}

func TestBroker_OnOrderBar_CancelledContext(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := submittedOrder(t, br, open, long, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, br.processOrder(ctx, fillBar(1, 111000), o), context.Canceled)
	assert.Equal(t, stateSubmitted, o.state)
}

// Documents current behavior, not a goal: when one order in a batch fails, the
// run stops before cleanup, so an order that already filled stays in
// account.orders. Live recovery will need to reconcile such partial batches;
// until then the backtest simply stops on the error.
func TestAccount_OnBar_ErrorMidBatchLeavesFilledOrdersListed(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	first := submittedOrder(t, br, open, long, 100)
	second := submittedOrder(t, br, open, long, 200) // fatal: the first one's position exists by then
	third := submittedOrder(t, br, open, long, 300)

	err := act.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.Equal(t, stateFilled, first.state, "processed before the failure")
	assert.Equal(t, stateSubmitted, second.state, "the failing order is untouched")
	assert.Equal(t, stateSubmitted, third.state, "later orders are not processed")
	assert.Equal(t, []*order{first, second, third}, act.orders["EURUSD"],
		"cleanup is skipped, so the filled order is still listed")
}
