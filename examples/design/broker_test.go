package main

import (
	"context"
	"math"
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
	assert.Equal(t, usd(10000), act.balance)
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
	act := b.addAccount("a1", usd(500))

	require.NotNil(t, act)
	assert.Equal(t, "a1", act.id)
	assert.Equal(t, usd(500), act.balance)

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, act, got)
}

func TestBroker_AddAccountReplacesSameID(t *testing.T) {
	b := &broker{id: "b"}
	b.addAccount("a1", usd(500))
	second := b.addAccount("a1", usd(900))

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, second, got)
	assert.Equal(t, usd(900), got.balance)
}

func TestBroker_GetAccountMissing(t *testing.T) {
	b := &broker{id: "b"}
	act, ok := b.getAccount("nope")
	assert.False(t, ok)
	assert.Nil(t, act)
}

func TestNewAccount(t *testing.T) {
	a := newAccount("x", usd(123))
	assert.Equal(t, "x", a.id)
	assert.Equal(t, usd(123), a.balance)
}

func TestBroker_SubmitMarksSubmittedAssignsIDAndLogs(t *testing.T) {
	logs := captureLogs(t)
	ts := time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)
	b := &broker{id: "sim"}
	b.addAccount("acct", usd(1000))
	o := &order{
		accountID:  "acct",
		instrument: "EURUSD",
		direction:  long,
		action:     open,
		orderType:  marketOrder,
		price:      eurusd1_10,
		stopLoss:   108900,
		Time:       ts,
	}

	require.NoError(t, b.submit(context.Background(), o))

	assert.Equal(t, "sim-1", o.id)
	assert.Equal(t, stateSubmitted, o.state)
	assert.Equal(t, int64(909), o.units, "the account sized it: floor($1000 / 1.1)")
	require.Len(t, o.history, 2)
	assert.Equal(t, stateSized, o.history[0].state)
	assert.Equal(t, stateSubmitted, o.history[1].state)
	assert.Equal(t, ts, o.history[1].time, "stamped with the order's bar time")
	out := logs.String()
	assert.Contains(t, out, "to=submitted", "the state change is logged")
	assert.Contains(t, out, "order submitted")
	assert.Contains(t, out, "broker=sim")
	assert.Contains(t, out, "account=acct")
	assert.Contains(t, out, "order=sim-1")
	assert.Contains(t, out, "instrument=EURUSD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
	assert.Contains(t, out, "type=market")
	assert.Contains(t, out, "units=909")
}

func TestBroker_SubmitIDsAreSequentialAndKept(t *testing.T) {
	captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", usd(1000))
	ctx := context.Background()

	o1, o2 := brokerOpen("acct"), &order{Time: testStart, accountID: "acct", instrument: "EURUSD", action: close}
	require.NoError(t, b.submit(ctx, o1))
	require.NoError(t, b.submit(ctx, o2))
	assert.Equal(t, "sim-1", o1.id)
	assert.Equal(t, "sim-2", o2.id)

	pre := brokerOpen("acct")
	pre.id = "mine"
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
	b.addAccount("acct", usd(1000))
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
	b.addAccount("acct", usd(1000))
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
	b.addAccount("one", usd(1000))
	b.addAccount("two", usd(1000))

	require.NoError(t, b.submit(context.Background(), brokerOpen("two")))

	assert.Contains(t, logs.String(), "account=two")
	assert.NotContains(t, logs.String(), "account=one")
}

// brokerOpen is a valid EURUSD open for the account.
func brokerOpen(accountID string) *order {
	return &order{
		Time: testStart, accountID: accountID, instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: 108900,
	}
}

// fillBar is a EURUSD bar on day d (0 = testStart) opening at o.
func fillBar(d int, o price) bar {
	return bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, d), ohlc: ohlc{o: o, h: o + 100, l: o, c: o + 100}}
}

// submitted is an order the broker has accepted on day 0.
func submittedOrder(t *testing.T, br *broker, a action, d direction) *order {
	t.Helper()
	o := openOrderAt("EURUSD", d, eurusd1_10)
	o.action, o.accountID, o.Time = a, "a", testStart
	if a != open {
		o.direction, o.stopLoss = noDirection, 0
	}
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func newFillBroker() (*broker, *account) {
	br := &broker{id: "t"}
	return br, br.addAccount("a", usd(10000))
}

func TestBroker_SubmitQueuesTheOrderOnTheAccount(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()

	o := submittedOrder(t, br, open, long)

	assert.Equal(t, []*order{o}, br.orders["EURUSD"])
}

func TestBroker_OnBar_MarketOrderFillsAtTheNextBarsOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submittedOrder(t, br, open, long)
	ctx := context.Background()

	require.NoError(t, br.onBar(ctx, fillBar(0, 111000)))
	assert.Equal(t, stateSubmitted, o.state, "never fills on the bar that produced it")
	assert.Empty(t, act.positions)

	require.NoError(t, br.onBar(ctx, fillBar(1, 111500)))
	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, price(111500), o.fillPrice, "the open of the next bar")
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.Time, "stamped with the fill bar")
	assert.Equal(t, stateFilled, o.history[len(o.history)-1].state)
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.history[len(o.history)-1].time)
	assert.Equal(t, &position{instrument: "EURUSD", direction: long, units: o.units, entryPrice: 111500, markPrice: 111600}, act.positions["EURUSD"])
	require.Len(t, br.orders["EURUSD"], 1, "the filled open left the queue; only its stop waits")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][0].reason)
}

func TestBroker_OnBar_IgnoresOtherInstrumentsAndUntouchedLimits(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	market := submittedOrder(t, br, open, long)
	limit := brokerOpen("a")
	limit.orderType = limitOrder
	require.NoError(t, br.submit(context.Background(), limit))

	other := fillBar(1, 111500)
	other.instrument = "GBPUSD"
	require.NoError(t, br.onBar(context.Background(), other))
	assert.Equal(t, stateSubmitted, market.state, "a bar for another instrument does not fill it")

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111500)))
	assert.Equal(t, stateFilled, market.state)
	assert.Equal(t, stateSubmitted, limit.state, "the limit is below the bar: not touched")
	require.Len(t, br.orders["EURUSD"], 2)
	assert.Same(t, limit, br.orders["EURUSD"][0])
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][1].reason, "the filled open's stop")
}

func TestBroker_OnBar_CloseTakesThePositionsSizeAndRemovesIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	open1 := submittedOrder(t, br, open, short)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110500)))
	require.Equal(t, stateFilled, open1.state)
	assert.Equal(t, price(110500), act.positions["EURUSD"].entryPrice, "the position remembers its fill price")

	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))
	require.NoError(t, br.onBar(ctx, fillBar(2, 112000)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, price(110500), open1.fillPrice)
	assert.Equal(t, open1.units, cl.units, "the order records what was closed")
	assert.Equal(t, price(112000), cl.fillPrice)
	assert.Empty(t, act.positions)
}

func TestBroker_OnBar_CloseWithoutAPositionIsRejectedNotFatal(t *testing.T) {
	logs := captureLogs(t)
	br, _ := newFillBroker()
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(context.Background(), cl))

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111000)))

	assert.Equal(t, stateRejected, cl.state)
	assert.Contains(t, cl.history[len(cl.history)-1].reason, "no position to close")
	assert.Zero(t, cl.fillPrice)
	assert.Contains(t, logs.String(), "order rejected")
	assert.Empty(t, br.orders["EURUSD"], "a rejected order is no longer waiting")
}

func TestBroker_OnBar_OpenOverAnExistingPositionIsFatal(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10}
	o := submittedOrder(t, br, open, long)

	err := br.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.False(t, isRejection(err))
	assert.Equal(t, stateSubmitted, o.state)
}

func TestAccount_ApplyFill_AddAndReduceAreUnsupported(t *testing.T) {
	act := newAccount("a", usd(1000))
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
	o := submittedOrder(t, br, open, long)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, br.processOrder(ctx, fillBar(1, 111000), o), context.Canceled)
	assert.Equal(t, stateSubmitted, o.state)
}

// Documents current behavior, not a goal: when one order in a batch fails, the
// run stops before cleanup, so an order that already filled stays in
// the broker's orders. Live recovery will need to reconcile such partial batches;
// until then the backtest simply stops on the error.
func TestBroker_OnBar_ErrorMidBatchLeavesFilledOrdersListed(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	first := submittedOrder(t, br, open, long)
	second := submittedOrder(t, br, open, long) // fatal: the first one's position exists by then
	third := submittedOrder(t, br, open, long)

	err := br.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.Equal(t, stateFilled, first.state, "processed before the failure")
	assert.Equal(t, stateSubmitted, second.state, "the failing order is untouched")
	assert.Equal(t, stateSubmitted, third.state, "later orders are not processed")
	require.Len(t, br.orders["EURUSD"], 4)
	assert.Equal(t, []*order{first, second, third}, br.orders["EURUSD"][:3],
		"cleanup is skipped, so the filled order is still listed")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][3].reason, "the first fill's stop")
}

func TestBroker_Submit_HasTheAccountSizeTheOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := brokerOpen("a")
	o.units = 7 // a strategy does not choose units; the account does

	require.NoError(t, br.submit(context.Background(), o))

	assert.Equal(t, int64(9090), o.units, "floor($10000 / 1.1)")
}

func TestBroker_Submit_SizingRejectionIsReturnedRecordedAndNotQueued(t *testing.T) {
	captureLogs(t)
	br := &broker{id: "t"}
	br.addAccount("a", 0)
	o := brokerOpen("a")

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errInsufficientMargin)
	assert.True(t, isRejection(err))
	assert.Equal(t, stateRejected, o.state)
	assert.Empty(t, o.id, "a rejected order never gets a broker id")
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_Submit_UnknownInstrumentIsFatal(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := brokerOpen("a")
	o.instrument = "XXXYYY"

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.False(t, isRejection(err))
	assert.Empty(t, br.orders["XXXYYY"])
}

func TestBroker_ReservedMargin(t *testing.T) {
	captureLogs(t)
	br := &broker{id: "t", orders: map[string][]*order{"EURUSD": {
		{accountID: "a", instrument: "EURUSD", action: open, units: 10000, price: 110000, state: stateSubmitted}, // $220
		{accountID: "a", instrument: "EURUSD", action: open, units: 5000, price: 110000, state: stateSubmitted},  // $110
		{accountID: "a", instrument: "EURUSD", action: close, state: stateSubmitted},                             // frees, never reserves
		{accountID: "a", instrument: "EURUSD", action: open, units: 99999, price: 110000, state: stateFilled},    // already a position
		{accountID: "a", instrument: "EURUSD", action: open, units: 99999, price: 110000, state: stateRejected},
		{accountID: "b", instrument: "EURUSD", action: open, units: 99999, price: 110000, state: stateSubmitted}, // another account's
	}}}

	got, err := br.reservedMargin("a")

	require.NoError(t, err)
	assert.Equal(t, usd(330), got)

	none, err := br.reservedMargin("nobody")
	require.NoError(t, err)
	assert.Zero(t, none)
}

func TestBroker_ReservedMargin_UnknownInstrumentAndOverflowAreErrors(t *testing.T) {
	br := &broker{id: "t", orders: map[string][]*order{"XXXYYY": {
		{accountID: "a", instrument: "XXXYYY", action: open, units: 1, price: 1, state: stateSubmitted},
	}}}
	_, err := br.reservedMargin("a")
	require.ErrorIs(t, err, errUnknownInstrument)

	br = &broker{id: "t", orders: map[string][]*order{"EURUSD": {
		{accountID: "a", instrument: "EURUSD", action: open, units: math.MaxInt64, price: math.MaxInt64, state: stateSubmitted},
	}}}
	_, err = br.reservedMargin("a")
	require.ErrorIs(t, err, errSizeOverflow)
}

func TestBroker_Submit_ReservedMarginShrinksTheNextOrder(t *testing.T) {
	captureLogs(t)
	br := &broker{id: "t"}
	act := br.addAccount("a", usd(10000))
	act.riskPct = rate(rateScale) // only the margin can limit the orders
	ctx := context.Background()

	first := brokerOpen("a")
	require.NoError(t, br.submit(ctx, first))
	assert.Equal(t, int64(454545), first.units, "all the margin")

	second := brokerOpen("a")
	err := br.submit(ctx, second)

	require.ErrorIs(t, err, errInsufficientMargin, "the first order holds the margin until it fills")
	assert.Equal(t, stateRejected, second.state)
}

func TestBroker_OnBar_BooksRealizedPnLToTheBalance(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, short)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110500)))
	require.Equal(t, usd(10000), act.balance, "an open realizes nothing")

	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))
	require.NoError(t, br.onBar(ctx, fillBar(2, 112000)))

	// A short from 1.10500 to 1.12000 loses 1500 points a unit: 1.5 cents a unit.
	assert.Equal(t, usd(10000)-money(op.units*3/2), act.balance)
}

func TestBroker_OnBar_RejectedCloseLeavesTheBalance(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(context.Background(), cl))

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111000)))

	assert.Equal(t, usd(10000), act.balance)
}

func TestBroker_OnBar_BalanceOverflowStopsTheFillAndKeepsThePosition(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 110000, markPrice: 110000}
	act.balance = money(math.MaxInt64)
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(ctx, cl))

	err := br.onBar(ctx, fillBar(1, 112000))

	require.ErrorIs(t, err, errSizeOverflow)
	assert.Equal(t, money(math.MaxInt64), act.balance)
	assert.Contains(t, act.positions, "EURUSD", "the position survives a failed close")
	assert.Equal(t, stateSubmitted, cl.state)
}

func TestBroker_OnBar_MarksEveryAccountsPosition(t *testing.T) {
	captureLogs(t)
	br := &broker{id: "t"}
	one := br.addAccount("one", usd(1))
	two := br.addAccount("two", usd(1))
	one.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 110000, markPrice: 110000}
	two.positions["EURUSD"] = &position{instrument: "EURUSD", direction: short, units: 1, entryPrice: 110000, markPrice: 110000}

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111500)))

	assert.Equal(t, price(111600), one.positions["EURUSD"].markPrice)
	assert.Equal(t, price(111600), two.positions["EURUSD"].markPrice)
}

func TestBroker_OnBar_AppliesFillsToTheOrdersOwnAccount(t *testing.T) {
	captureLogs(t)
	br := &broker{id: "t"}
	one := br.addAccount("one", usd(10000))
	two := br.addAccount("two", usd(10000))
	o := brokerOpen("two")
	require.NoError(t, br.submit(context.Background(), o))

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111000)))

	assert.Empty(t, one.positions)
	assert.Contains(t, two.positions, "EURUSD")
}

func TestBroker_Submit_RefusesOpensOnTheBarOfAMarginCloseout(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	br.closedOut = map[string]time.Time{"a": testStart}

	o := brokerOpen("a")
	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errCloseoutGuard)
	assert.True(t, isRejection(err))
	assert.Empty(t, br.orders["EURUSD"])

	// A close is still allowed, and so is an open on a later bar.
	cl := &order{Time: testStart, accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder}
	require.NoError(t, br.submit(context.Background(), cl))
	later := brokerOpen("a")
	later.Time = testStart.AddDate(0, 0, 1)
	require.NoError(t, br.submit(context.Background(), later))
}

func TestBroker_OnBar_CloseoutBlocksTheStrategyOpenOnTheSameBar(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.balance = usd(1000)
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000,
		entryPrice: eurusd1_10, markPrice: eurusd1_10}
	b := fillBar(1, 90000) // a crash: the account is liquidated on this bar

	require.NoError(t, br.onBar(context.Background(), b))
	require.Empty(t, act.positions, "liquidated")

	o := brokerOpen("a")
	o.Time = b.Time
	require.ErrorIs(t, br.submit(context.Background(), o), errCloseoutGuard)
}

func TestBroker_OnBar_RetainsCompletedOrdersInCompletionOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	ctx := context.Background()
	filled := submittedOrder(t, br, open, long)
	limit := brokerOpen("a")
	limit.orderType = limitOrder
	require.NoError(t, br.submit(ctx, limit))
	assert.Empty(t, br.completed, "nothing has completed yet")

	require.NoError(t, br.onBar(ctx, fillBar(1, 111500)))
	cl := submittedOrder(t, br, close, noDirection)
	cl.Time = fillBar(1, 0).Time
	require.NoError(t, br.onBar(ctx, fillBar(2, 112000)))

	require.Len(t, br.completed, 3)
	assert.Same(t, filled, br.completed[0], "filled orders are kept, in the order they completed")
	assert.Same(t, cl, br.completed[1])
	assert.Equal(t, stateCancelled, br.completed[2].state, "the open's stop, cancelled by the close")
	assert.Equal(t, []*order{limit}, br.orders["EURUSD"], "a waiting order is not completed")
	assert.Empty(t, br.closeouts(), "strategy orders are not closeouts")
}

func TestBroker_OnBar_RetainsRejectedOrders(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	cl := submittedOrder(t, br, close, noDirection) // no position: rejected on fill

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111500)))

	assert.Equal(t, stateRejected, cl.state)
	assert.Equal(t, []*order{cl}, br.completed)
}
