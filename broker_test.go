package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroker_AddAccount(t *testing.T) {
	b := newBroker("b", testMarkets())
	act := b.addAccount("a1", usd(500))

	require.NotNil(t, act)
	assert.Equal(t, "a1", act.id)
	assert.Equal(t, usd(500), act.balance)

	assert.Same(t, act, b.accounts["a1"])
}

func TestBroker_AddAccountReplacesSameID(t *testing.T) {
	b := newBroker("b", testMarkets())
	b.addAccount("a1", usd(500))
	second := b.addAccount("a1", usd(900))

	assert.Same(t, second, b.accounts["a1"])
	assert.Equal(t, usd(900), second.balance)
}

func TestNewAccount(t *testing.T) {
	a := newAccount("x", usd(123), testMarkets())
	assert.Equal(t, "x", a.id)
	assert.Equal(t, usd(123), a.balance)
}

func TestBroker_SubmitMarksSubmittedAssignsIDAndLogs(t *testing.T) {
	logs := captureLogs(t)
	ts := time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)
	b := newBroker("sim", testMarkets())
	b.addAccount("acct", usd(1000))
	o := &order{
		accountID:  "acct",
		instrument: "EURUSD",
		direction:  long,
		action:     open,
		orderType:  marketOrder,
		price:      eurusd1_10,
		stopLoss:   px(108900),
		Time:       ts,
	}

	require.NoError(t, b.submit(context.Background(), o))

	assert.Equal(t, "sim-1", o.id)
	assert.Equal(t, stateSubmitted, o.state)
	assert.Equal(t, qty(909), o.units, "the account sized it: floor($1000 / 1.1)")
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
	b := newBroker("sim", testMarkets())
	b.addAccount("acct", usd(1000))
	ctx := context.Background()

	o1, o2 := brokerOpen("acct"), &order{Time: testStart, accountID: "acct", instrument: "EURUSD", action: close}
	require.NoError(t, b.submit(ctx, o1))
	require.NoError(t, b.submit(ctx, o2))
	assert.Equal(t, "sim-1", o1.id)
	assert.Equal(t, "sim-2", o2.id)

	b.addAccount("other", usd(1000))
	pre := brokerOpen("other")
	pre.id = "mine"
	require.NoError(t, b.submit(ctx, pre))
	assert.Equal(t, "mine", pre.id, "an existing id is not overwritten")
}

func TestBroker_SubmitUnknownAccount(t *testing.T) {
	logs := captureLogs(t)
	b := newBroker("sim", testMarkets())
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
	b := newBroker("sim", testMarkets())
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
	b := newBroker("sim", testMarkets())
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
	b := newBroker("sim", testMarkets())
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
		orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900),
	}
}

// fillBar is a EURUSD bar on day d (0 = testStart) opening at o.
func fillBar(d int, oN int64) marketdata.Bar {
	o := px(oN)
	return daily(marketdata.Bar{Instrument: "EURUSD", Time: testStart.AddDate(0, 0, d), Mid: marketdata.OHLC{Open: o, High: px(oN + 100), Low: o, Close: px(oN + 100)}})
}

// submitted is an order the broker has accepted on day 0.
func submittedOrder(t *testing.T, br *broker, a action, d direction) *order {
	t.Helper()
	o := openOrderAt("EURUSD", d, eurusd1_10)
	o.action, o.accountID, o.Time = a, "a", testStart
	if a != open {
		o.direction, o.stopLoss = noDirection, num.Price{}
	}
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func newFillBroker() (*broker, *account) {
	br := newBroker("t", testMarkets())
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
	assert.Equal(t, px(111500), o.fillPrice, "the open of the next bar")
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.Time, "stamped with the fill bar")
	assert.Equal(t, stateFilled, o.history[len(o.history)-1].state)
	assert.Equal(t, testStart.AddDate(0, 0, 1), o.history[len(o.history)-1].time)
	assert.Equal(t, &position{instrument: "EURUSD", direction: long, units: o.units, entryPrice: px(111500), entryTime: o.Time, markPrice: px(111600),
		openOrder: o.id, plannedRisk: o.risk.planned}, act.positions["EURUSD"])
	require.Len(t, br.orders["EURUSD"], 1, "the filled open left the queue; only its stop waits")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][0].reason)
}

func TestBroker_OnBar_IgnoresOtherInstrumentsAndUntouchedLimits(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	market := submittedOrder(t, br, open, long)
	br.addAccount("b", usd(10000))
	limit := brokerOpen("b")
	limit.orderType = limitOrder
	require.NoError(t, br.submit(context.Background(), limit))

	other := fillBar(1, 111500)
	other.Instrument = "GBPUSD"
	require.NoError(t, br.onBar(context.Background(), other))
	assert.Equal(t, stateSubmitted, market.state, "a bar for another instrument does not fill it")

	require.NoError(t, br.onBar(context.Background(), fillBar(2, 111500)))
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
	assert.Equal(t, px(110500), act.positions["EURUSD"].entryPrice, "the position remembers its fill price")

	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))
	require.NoError(t, br.onBar(ctx, fillBar(2, 112000)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, px(110500), open1.fillPrice)
	assert.Equal(t, open1.units, cl.units, "the order records what was closed")
	assert.Equal(t, px(112000), cl.fillPrice)
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
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10)}
	o := submittedOrder(t, br, open, long)

	err := br.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.False(t, isRejection(err))
	assert.Equal(t, stateSubmitted, o.state)
}

func TestAccount_ApplyFill_AddAndReduceAreUnsupported(t *testing.T) {
	act := newAccount("a", usd(1000), testMarkets())
	for _, a := range []action{add, reduce} {
		require.ErrorIs(t, fillOn(act, &order{instrument: "EURUSD", action: a}), errUnsupportedAction, a.String())
	}
}

func TestBroker_Fill_UnknownAccount(t *testing.T) {
	br := newBroker("t", testMarkets())
	err := br.fill(&order{id: "t-1", accountID: "ghost", instrument: "EURUSD", action: open}, px(1), time.Time{})
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

// With no order waiting nothing else consults the context, so onFrame must: a
// cancelled run neither marks nor closes anything and reports the cancellation.
func TestBroker_OnFrame_CancelledContextWithNothingQueued(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	b := fillBar(1, 111000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := br.clock

	err := br.onFrame(ctx, frame{availableAt: b.AvailableAt, bars: []marketdata.Bar{b}})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, clock, br.clock, "no state moved")
}

// Documents current behavior, not a goal: when one order in a batch fails, the
// run stops before cleanup, so an order that already filled stays in
// the broker's orders. Live recovery will need to reconcile such partial batches;
// until then the backtest simply stops on the error.
func TestBroker_OnBar_ErrorMidBatchKeepsTheQueueConsistent(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	first := submittedOrder(t, br, open, long)
	// Queued directly: submit refuses a second pending open, but a fill can
	// still meet an existing position (fatal: the first one's position exists by then).
	queued := func(id string) *order {
		o := brokerOpen("a")
		o.id, o.units, o.state = id, qty(100), stateSubmitted
		br.orders["EURUSD"] = append(br.orders["EURUSD"], o)
		return o
	}
	second := queued("t-q2")
	third := queued("t-q3")

	err := br.onBar(context.Background(), fillBar(1, 111000))

	require.ErrorIs(t, err, errPositionExists)
	assert.Equal(t, stateFilled, first.state, "processed before the failure")
	assert.Equal(t, stateSubmitted, second.state, "the failing order is untouched")
	assert.Equal(t, stateSubmitted, third.state, "later orders are not processed")
	// The queue holds exactly the waiting orders even after a failure: the
	// filled order left it the moment it completed, before the error.
	require.Len(t, br.orders["EURUSD"], 3)
	assert.Equal(t, []*order{second, third}, br.orders["EURUSD"][:2], "the unprocessed orders still wait")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][2].reason, "with the first fill's stop")
	assert.Equal(t, []*order{first}, br.completed)
}

func TestBroker_Submit_HasTheAccountSizeTheOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := brokerOpen("a")
	o.units = qty(7) // a strategy does not choose units; the account does

	require.NoError(t, br.submit(context.Background(), o))

	assert.Equal(t, qty(9090), o.units, "floor($10000 / 1.1)")
}

func TestBroker_Submit_SizingRejectionIsReturnedRecordedAndNotQueued(t *testing.T) {
	captureLogs(t)
	br := newBroker("t", testMarkets())
	br.addAccount("a", usd(0))
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
	br := &broker{id: "t", markets: testMarkets(), orders: map[string][]*order{"EURUSD": {
		{accountID: "a", instrument: "EURUSD", action: open, units: qty(10000), price: px(110000), state: stateSubmitted}, // $220
		{accountID: "a", instrument: "EURUSD", action: open, units: qty(5000), price: px(110000), state: stateSubmitted},  // $110
		{accountID: "a", instrument: "EURUSD", action: close, state: stateSubmitted},                                      // frees, never reserves
		{accountID: "b", instrument: "EURUSD", action: open, units: qty(99999), price: px(110000), state: stateSubmitted}, // another account's
	}}}

	got := reservedOf(t, br, "a")
	assert.Equal(t, usd(330), got)

	none, err := br.reservedMargin(&account{id: "nobody", currency: usdCurrency})
	require.NoError(t, err)
	assert.True(t, none.IsZero())
}

func TestBroker_Submit_ReservedMarginShrinksTheNextOrder(t *testing.T) {
	cat := testMarkets()
	captureLogs(t)
	br := newBroker("t", cat)
	act := br.addAccount("a", usd(10000))
	act.riskPct = rateOne // only the margin can limit the orders
	ctx := context.Background()

	first := brokerOpen("a")
	require.NoError(t, br.submit(ctx, first))
	assert.Equal(t, qty(454545), first.units, "all the margin")

	second := brokerOpen("a")
	second.instrument = "GBPUSD" // another instrument, so only the margin can refuse it
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
	assert.Equal(t, msub(usd(10000), cents(whole(op.units)*3/2)), act.balance)
}

func TestBroker_OnBar_RejectedCloseLeavesTheBalance(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(context.Background(), cl))

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111000)))

	assert.Equal(t, usd(10000), act.balance)
}

func TestBroker_OnBar_MarksEveryAccountsPosition(t *testing.T) {
	captureLogs(t)
	br := newBroker("t", testMarkets())
	one := br.addAccount("one", usd(1))
	two := br.addAccount("two", usd(1))
	one.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(1), entryPrice: px(110000), markPrice: px(110000)}
	two.positions["EURUSD"] = &position{instrument: "EURUSD", direction: short, units: qty(1), entryPrice: px(110000), markPrice: px(110000)}

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 111500)))

	assert.Equal(t, px(111600), one.positions["EURUSD"].markPrice)
	assert.Equal(t, px(111600), two.positions["EURUSD"].markPrice)
}

func TestBroker_OnBar_AppliesFillsToTheOrdersOwnAccount(t *testing.T) {
	captureLogs(t)
	br := newBroker("t", testMarkets())
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
	br, act := newFillBroker()
	closeoutBar := fillBar(0, points(eurusd1_10))
	act.closedOutAt = closeoutBar.AvailableAt

	o := brokerOpen("a")
	o.decidedAt = closeoutBar.AvailableAt // decided on the closeout bar
	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errCloseoutGuard)
	assert.True(t, isRejection(err))
	assert.Empty(t, br.orders["EURUSD"])

	// A close is still allowed, and so is an open on a later bar.
	cl := &order{Time: testStart, accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder}
	require.NoError(t, br.submit(context.Background(), cl))
	next := fillBar(1, points(eurusd1_10))
	later := brokerOpen("a")
	later.Time, later.decidedAt = next.Time, next.AvailableAt // decided on the next bar
	require.NoError(t, br.submit(context.Background(), later))
}

func TestBroker_OnBar_CloseoutBlocksTheStrategyOpenOnTheSameBar(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.balance = usd(1000)
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10000),
		entryPrice: eurusd1_10, markPrice: eurusd1_10}
	b := fillBar(1, 90000) // a crash: the account is liquidated on this bar

	require.NoError(t, br.onBar(context.Background(), b))
	require.Empty(t, act.positions, "liquidated")

	o := brokerOpen("a")
	o.Time, o.decidedAt = b.Time, b.AvailableAt // decided on the closeout bar
	require.ErrorIs(t, br.submit(context.Background(), o), errCloseoutGuard)
}

func TestBroker_OnBar_RetainsCompletedOrdersInCompletionOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	ctx := context.Background()
	filled := submittedOrder(t, br, open, long)
	br.addAccount("b", usd(10000))
	limit := brokerOpen("b")
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

func TestBroker_OnBar_MarginCloseoutRecordsATrade(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.balance = usd(1000)
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10000),
		entryPrice: eurusd1_10, entryTime: testStart, markPrice: eurusd1_10}
	b := fillBar(1, 90000)

	require.NoError(t, br.onBar(context.Background(), b))

	assert.Equal(t, []trade{{instrument: "EURUSD", direction: long, units: qty(10000),
		entryPrice: eurusd1_10, entryTime: testStart, exitPrice: b.OHLC().Close, exitTime: b.Time,
		reason: reasonMarginCloseout, pnl: mneg(usd(1990))}}, act.trades)
}

func TestOpenOnly_CollapsesEachPresentSeriesToItsOwnOpenAndInventsNone(t *testing.T) {
	flat := func(o num.Price) marketdata.OHLC { return marketdata.OHLC{Open: o, High: o, Low: o, Close: o} }
	bid := marketdata.OHLC{Open: px(109990), High: px(110390), Low: px(109490), Close: px(110190)}
	ask := marketdata.OHLC{Open: px(110010), High: px(110410), Low: px(109510), Close: px(110210)}
	mid, err := marketdata.MidOf(bid, ask)
	require.NoError(t, err)
	trade := marketdata.OHLC{Open: px(110005), High: px(110300), Low: px(109600), Close: px(110100)}
	open := testStart
	mk := func(mutate func(*marketdata.Bar)) marketdata.Bar {
		b := marketdata.Bar{Instrument: "EURUSD", Time: open, AvailableAt: open.Add(time.Hour), Volume: 7, Bid: bid, Ask: ask, Mid: mid}
		mutate(&b)
		return b
	}
	for name, tc := range map[string]struct {
		in   marketdata.Bar
		want marketdata.Bar
	}{
		"mid only": {
			mk(func(b *marketdata.Bar) { b.Bid, b.Ask = marketdata.OHLC{}, marketdata.OHLC{} }),
			mk(func(b *marketdata.Bar) { b.Bid, b.Ask, b.Mid = marketdata.OHLC{}, marketdata.OHLC{}, flat(mid.Open) }),
		},
		"bid only: no mid is made up": {
			mk(func(b *marketdata.Bar) { b.Ask, b.Mid = marketdata.OHLC{}, marketdata.OHLC{} }),
			mk(func(b *marketdata.Bar) { b.Ask, b.Mid, b.Bid = marketdata.OHLC{}, marketdata.OHLC{}, flat(bid.Open) }),
		},
		"ask only: no mid is made up": {
			mk(func(b *marketdata.Bar) { b.Bid, b.Mid = marketdata.OHLC{}, marketdata.OHLC{} }),
			mk(func(b *marketdata.Bar) { b.Bid, b.Mid, b.Ask = marketdata.OHLC{}, marketdata.OHLC{}, flat(ask.Open) }),
		},
		"bid, ask and mid": {
			mk(func(*marketdata.Bar) {}),
			mk(func(b *marketdata.Bar) { b.Bid, b.Ask, b.Mid = flat(bid.Open), flat(ask.Open), flat(mid.Open) }),
		},
		"trade only": {
			mk(func(b *marketdata.Bar) {
				b.Bid, b.Ask, b.Mid, b.Trade = marketdata.OHLC{}, marketdata.OHLC{}, marketdata.OHLC{}, trade
			}),
			mk(func(b *marketdata.Bar) {
				b.Bid, b.Ask, b.Mid, b.Trade = marketdata.OHLC{}, marketdata.OHLC{}, marketdata.OHLC{}, flat(trade.Open)
			}),
		},
		"every series, trade too": {
			mk(func(b *marketdata.Bar) { b.Trade = trade }),
			mk(func(b *marketdata.Bar) {
				b.Bid, b.Ask, b.Mid, b.Trade = flat(bid.Open), flat(ask.Open), flat(mid.Open), flat(trade.Open)
			}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			original := tc.in
			require.NoError(t, original.Validate(), "the fixture is a valid bar")

			got := openOnly(tc.in)

			assert.Equal(t, tc.want, got)
			assert.NoError(t, got.Validate(), "the open-only copy of a valid bar is valid")
			assert.Equal(t, original, tc.in, "the original is untouched")
			assert.Equal(t, original.OHLC().Open, got.OHLC().Open, "the engine's price at the open is the same")
			assert.Equal(t, got.OHLC().Open, got.OHLC().Close)
		})
	}
}

// fixedEntry fills a market open at 1.10000 on day 1 with the 1% stop of
// openOrderAt (1.08900 for a long, 1.11100 for a short), no trail and no
// target, and returns the account and the resting stop.
func fixedEntry(t *testing.T, br *broker, act *account, d direction) *order {
	t.Helper()
	op := openOrderAt("EURUSD", d, eurusd1_10)
	op.accountID = act.id
	require.NoError(t, br.submit(context.Background(), op))
	require.NoError(t, br.onBar(context.Background(), dayBar(1, 110000, 110000, 110000, 110000)))
	require.Equal(t, stateFilled, op.state)
	require.Len(t, br.orders["EURUSD"], 1)
	stop := br.orders["EURUSD"][0]
	require.Equal(t, reasonStopLoss, stop.reason)
	require.True(t, stop.trail.IsZero())
	return stop
}

// amendOrder is an amend of the account's EURUSD position, decided on day 1.
func amendOrder(stopN, targetN, trailN int64) *order {
	o := &order{Time: day(1), accountID: "a", instrument: "EURUSD", action: amend, orderType: marketOrder}
	if stopN != 0 {
		o.stopLoss = px(stopN)
	}
	if targetN != 0 {
		o.takeProfit = px(targetN)
	}
	if trailN != 0 {
		o.trail = px(trailN)
	}
	return o
}

func TestBroker_Amend_MovesALongsStopUpAndKeepsTheHistory(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	o := amendOrder(109500, 0, 0)

	require.NoError(t, br.submit(context.Background(), o))

	assert.Equal(t, px(109500), stop.price)
	assert.Equal(t, stateSubmitted, stop.state, "the stop is still resting")
	assert.Equal(t, "stop amended from 1.089 to 1.095 by "+o.id, stop.history[len(stop.history)-1].reason)
	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, "stop 1.089 to 1.095", o.history[len(o.history)-1].reason)
	assert.NotEmpty(t, o.id)
	assert.Equal(t, []*order{o}, br.amended)
	assert.NotContains(t, br.completed, o, "an amend is not an execution")
}

func TestBroker_Amend_MovesAShortsStopDown(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, short)
	require.Equal(t, px(111100), stop.price)

	require.NoError(t, br.submit(context.Background(), amendOrder(110500, 0, 0)))

	assert.Equal(t, px(110500), stop.price)
}

func TestBroker_Amend_NeverLoosensAStop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		d     direction
		stopN int64
	}{
		{"long stop moved down", long, 108000},
		{"long stop unchanged", long, 108900},
		{"short stop moved up", short, 112000},
		{"short stop unchanged", short, 111100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			br, act := newFillBroker()
			stop := fixedEntry(t, br, act, tc.d)
			before := stop.price
			o := amendOrder(tc.stopN, 0, 0)

			err := br.submit(context.Background(), o)

			require.ErrorIs(t, err, errLoosensStop)
			assert.True(t, isRejection(err), "the run goes on")
			assert.Equal(t, stateRejected, o.state)
			assert.Equal(t, before, stop.price)
			assert.Empty(t, br.amended)
		})
	}
}

func TestBroker_Amend_AStopMustBeOnTheLosingSideOfTheLatestPrice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		d     direction
		stopN int64
	}{
		{"long stop at the price", long, 110000},
		{"long stop above the price", long, 111000},
		{"short stop at the price", short, 110000},
		{"short stop below the price", short, 109000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			br, act := newFillBroker()
			stop := fixedEntry(t, br, act, tc.d)
			before := stop.price

			err := br.submit(context.Background(), amendOrder(tc.stopN, 0, 0))

			require.True(t, isRejection(err), "%v", err)
			assert.Equal(t, before, stop.price)
		})
	}
}

func TestBroker_Amend_ALongsStopCanLockInProfitPastTheEntry(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110000, 112000, 110000, 111500)))

	require.NoError(t, br.submit(context.Background(), amendOrder(110500, 0, 0)))

	assert.Equal(t, px(110500), stop.price, "above the 1.10000 entry, below the 1.11500 latest price")
}

func TestBroker_Amend_TurnsAFixedStopIntoATrailingOne(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	o := amendOrder(0, 0, 1500)

	require.NoError(t, br.submit(context.Background(), o))

	assert.Equal(t, px(108900), stop.price, "the stop keeps its level now")
	assert.Equal(t, px(1500), stop.trail)
	assert.Equal(t, "trail amended from 0 to 0.015 by "+o.id, stop.history[len(stop.history)-1].reason)

	// From the next bar it ratchets toward profit, and never back.
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110500, 112000, 110200, 111500)))
	assert.Equal(t, px(110500), stop.price)
	require.NoError(t, br.onBar(context.Background(), dayBar(3, 111000, 111500, 110600, 111000)))
	assert.Equal(t, px(110500), stop.price)
	assert.Contains(t, act.positions, "EURUSD")
}

func TestBroker_Amend_ATrailCanOnlyBeShortened(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trailN  int64
		wantErr bool
	}{
		{"shorter", 1000, false},
		{"equal", 1500, true},
		{"longer", 2500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			br, _ := newFillBroker()
			_, stop := trailingEntry(t, br, long, 1500)

			err := br.submit(context.Background(), amendOrder(0, 0, tc.trailN))

			if tc.wantErr {
				require.ErrorIs(t, err, errLoosensStop)
				assert.Equal(t, px(1500), stop.trail)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, px(tc.trailN), stop.trail)
		})
	}
}

func TestBroker_Amend_StopAndTrailTogetherApplyTheStopFirst(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	o := amendOrder(109500, 0, 1000)

	require.NoError(t, br.submit(context.Background(), o))

	assert.Equal(t, px(109500), stop.price)
	assert.Equal(t, px(1000), stop.trail)
	assert.Equal(t, "stop 1.089 to 1.095, trail 0 to 0.01", o.history[len(o.history)-1].reason)
}

func TestBroker_Amend_AddsAndMovesATakeProfit(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	fixedEntry(t, br, act, long)
	targets := func() []*order {
		var out []*order
		for _, x := range br.orders["EURUSD"] {
			if x.reason == reasonTakeProfit {
				out = append(out, x)
			}
		}
		return out
	}
	require.Empty(t, targets())

	require.NoError(t, br.submit(context.Background(), amendOrder(0, 112000, 0)))
	require.Len(t, targets(), 1)
	target := targets()[0]
	assert.Equal(t, px(112000), target.price)
	assert.Equal(t, limitOrder, target.orderType)
	assert.Equal(t, close, target.action)
	assert.Equal(t, long, target.direction, "the direction of the position it closes")

	// A target may move either way on the winning side: it carries no risk.
	require.NoError(t, br.submit(context.Background(), amendOrder(0, 111000, 0)))
	assert.Equal(t, px(111000), target.price)
	require.NoError(t, br.submit(context.Background(), amendOrder(0, 113000, 0)))
	assert.Equal(t, px(113000), target.price)
	assert.Len(t, targets(), 1)

	// The target then fills like any target.
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 112000, 113500, 111900, 113000)))
	assert.Equal(t, stateFilled, target.state)
	assert.Equal(t, px(113000), target.fillPrice)
	assert.Empty(t, act.positions)
	assert.Empty(t, br.orders["EURUSD"], "the stop went with the position")
}

func TestBroker_Amend_ATargetMustBeOnTheWinningSideOfTheLatestPrice(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	fixedEntry(t, br, act, long)

	err := br.submit(context.Background(), amendOrder(0, 109000, 0))

	require.ErrorIs(t, err, errStopWrongSide)
	assert.True(t, isRejection(err))
	assert.Len(t, br.orders["EURUSD"], 1, "no target was placed")
}

func TestBroker_Amend_ARefusedChangeChangesNothing(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)

	// A good stop and an impossible target: all or nothing.
	err := br.submit(context.Background(), amendOrder(109500, 109000, 1000))

	require.True(t, isRejection(err))
	assert.Equal(t, px(108900), stop.price)
	assert.True(t, stop.trail.IsZero())
	assert.Len(t, br.orders["EURUSD"], 1)
	assert.Empty(t, br.amended)
}

func TestBroker_Amend_WithoutAPositionIsRejectedNotFatal(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := amendOrder(109500, 0, 0)

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errNoPosition)
	assert.True(t, isRejection(err))
	assert.Equal(t, stateRejected, o.state)
}

func TestBroker_Amend_AfterTheStopClosedThePositionIsRejected(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	fixedEntry(t, br, act, long)
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 109000, 109100, 108000, 108500)))
	require.Empty(t, act.positions)

	err := br.submit(context.Background(), amendOrder(109000, 0, 0))

	require.ErrorIs(t, err, errNoPosition)
}

func TestBroker_Amend_OffTickPricesAreAFailureNotARejection(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	fixedEntry(t, br, act, long)
	o := amendOrder(0, 0, 0)
	o.stopLoss = num.MustParsePrice("1.0950001")

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errInvalidOrder)
	assert.False(t, isRejection(err), "a strategy bug stops the run")
}

func TestBroker_Amend_UnknownInstrumentAndCancelledContext(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	fixedEntry(t, br, act, long)

	o := amendOrder(109500, 0, 0)
	o.instrument = "NOSUCH"
	require.ErrorIs(t, br.submit(context.Background(), o), errUnknownInstrument)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, br.submit(ctx, amendOrder(109500, 0, 0)), context.Canceled)
}

func TestBroker_Amend_OnlyChangesTheOrdersOwnAccount(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	other := br.addAccount("b", usd(10000))
	o := amendOrder(109500, 0, 0)
	o.accountID = other.id

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, errNoPosition, "b has no position")
	assert.Equal(t, px(108900), stop.price, "a's stop is untouched")
}

func TestBroker_Amend_TheNewStopTriggersOnALaterBarAtItsLevel(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	require.NoError(t, br.submit(context.Background(), amendOrder(109500, 0, 0)))

	// Day 2 trades down through 1.09500 but never to the old 1.08900.
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110000, 110200, 109400, 109600)))

	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, px(109500), stop.fillPrice)
	assert.Empty(t, act.positions)
}

func TestBroker_Amend_ADecidedStopDoesNotTriggerOnABarThatOpenedBeforeIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	o := amendOrder(109500, 0, 0)
	o.Time = day(3) // decided on a later bar than the bar below
	require.NoError(t, br.submit(context.Background(), o))

	_, ok := stop.execution(dayBar(2, 110000, 110200, 109000, 109600))

	assert.False(t, ok, "the bar opened before the amend was decided")
	assert.Equal(t, day(3), stop.Time)
}

func TestBroker_Amend_ExitHistoryRebuildsTheStopAtEveryChange(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	stop := fixedEntry(t, br, act, long)
	first, second := amendOrder(109500, 0, 0), amendOrder(109800, 0, 1000)
	require.NoError(t, br.submit(context.Background(), first))
	require.NoError(t, br.submit(context.Background(), second))

	var reasons []string
	for _, e := range stop.history {
		if e.reason != "" {
			reasons = append(reasons, e.reason)
		}
	}

	assert.Equal(t, []string{
		"stop amended from 1.089 to 1.095 by " + first.id,
		"stop amended from 1.095 to 1.098 by " + second.id,
		"trail amended from 0 to 0.01 by " + second.id,
	}, reasons, "each change names the amend and both values")
	assert.NotEqual(t, first.id, second.id)
}

// The #743 audit tests. A name with KnownDefect743 characterizes unsafe current
// behaviour and links the issue that will replace its assertions; the others are
// safety invariants that a fix must keep. See docs/reviews/risk-audit-743.org.

// riskOpen submits a EURUSD market or resting open on 10,000, decided on day 0.
func riskOpen(t *testing.T, br *broker, d direction, typ orderType, ref, stop int64) *order {
	t.Helper()
	o := &order{Time: testStart, accountID: "a", instrument: "EURUSD", direction: d, action: open,
		orderType: typ, price: px(ref), stopLoss: px(stop)}
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

// riskBar is a EURUSD bar on day d.
func riskBar(d int, o, h, l, c int64) marketdata.Bar {
	return daily(marketdata.Bar{Instrument: "EURUSD", Time: testStart.AddDate(0, 0, d),
		Mid: marketdata.OHLC{Open: px(o), High: px(h), Low: px(l), Close: px(c)}})
}

// Units come from the reference price and are never resized, so an entry that
// fills further from its stop than the reference loses more than 1% there.
//
// KNOWN DEFECT, not a safety invariant: these assertions pin unsafe behaviour and
// must be replaced by tests of the safe behaviour when #746 is fixed.
func TestBroker_KnownDefect743_AFillAwayFromTheReferenceExceedsOnePercentAtTheStop(t *testing.T) {
	captureLogs(t)
	for _, tc := range []struct {
		name      string
		dir       direction
		typ       orderType
		ref, stop int64
		entry     marketdata.Bar
		exit      marketdata.Bar
		fill      int64
		balance   money
	}{
		{"long market, higher open", long, marketOrder, 110000, 109450,
			riskBar(1, 110550, 110600, 110500, 110550), riskBar(2, 110500, 110500, 109400, 109450),
			110550, num.MustParseMoney("9800.009", usdCurrency)},
		{"short market, lower open", short, marketOrder, 110000, 110550,
			riskBar(1, 109450, 109500, 109400, 109450), riskBar(2, 109500, 110600, 109500, 110550),
			109450, num.MustParseMoney("9800.009", usdCurrency)},
		{"long stop entry gapped past its trigger", long, stopOrder, 110000, 109450,
			riskBar(1, 110550, 110600, 110500, 110550), riskBar(2, 110500, 110500, 109400, 109450),
			110550, num.MustParseMoney("9800.009", usdCurrency)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br, act := newFillBroker()
			ctx := context.Background()
			o := riskOpen(t, br, tc.dir, tc.typ, tc.ref, tc.stop)
			require.Equal(t, qty(18181), o.units, "plans 99.9955 from the reference")

			require.NoError(t, br.onBar(ctx, tc.entry))
			require.Equal(t, px(tc.fill), o.fillPrice)
			require.NoError(t, br.onBar(ctx, tc.exit))

			require.Empty(t, act.positions)
			assert.Equal(t, px(tc.stop), br.completed[len(br.completed)-1].fillPrice, "the stop filled at its own price")
			assert.Equal(t, tc.balance, act.balance, "twice the planned loss")
		})
	}
}

// A stop gapped through fills at the open: the loss has no bound from the plan.
// The tighter the stop, the larger the position and the worse the same gap.
//
// KNOWN DEFECT, not a safety invariant: these assertions pin unsafe behaviour and
// must be replaced by tests of the safe behaviour when #747 is fixed.
// The cap changes the margin-capped case; a gap itself cannot be prevented.
func TestBroker_KnownDefect743_AGapThroughTheStopScalesWithPositionSize(t *testing.T) {
	captureLogs(t)
	for _, tc := range []struct {
		name    string
		stop    int64
		units   int64
		balance string
	}{
		{"0.5% stop", 109450, 18181, "9800.009"},
		{"0.01% stop, margin capped", 109989, 454545, "5000.005"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br, act := newFillBroker()
			ctx := context.Background()
			o := riskOpen(t, br, long, marketOrder, 110000, tc.stop)
			require.Equal(t, qty(tc.units), o.units)
			require.NoError(t, br.onBar(ctx, riskBar(1, 110000, 110000, 110000, 110000)))

			// The same 1% gap: the next bar opens at 1.08900.
			require.NoError(t, br.onBar(ctx, riskBar(2, 108900, 108900, 108900, 108900)))

			require.Empty(t, act.positions)
			assert.Equal(t, num.MustParseMoney(tc.balance, usdCurrency), act.balance)
		})
	}
}

// A margin-capped order that fills at a worse price is refused, not admitted
// over the margin.
func TestBroker_AMarginCappedOpenIsRefusedAtAHigherFill(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := riskOpen(t, br, long, marketOrder, 110000, 109989)

	require.NoError(t, br.onBar(context.Background(), riskBar(1, 110001, 110001, 110001, 110001)))

	assert.Equal(t, stateRejected, o.state)
	assert.Empty(t, act.positions)
}

// The 1% budget is per order: nothing limits the sum across instruments.
//
// KNOWN DEFECT, not a safety invariant: these assertions pin unsafe behaviour and
// must be replaced by tests of the safe behaviour when #748 is fixed.
func TestBroker_KnownDefect743_PlannedRiskAddsUpAcrossInstruments(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	ctx := context.Background()
	first := brokerOpen("a")
	second := brokerOpen("a")
	second.instrument = "GBPUSD"

	require.NoError(t, br.submit(ctx, first))
	require.NoError(t, br.submit(ctx, second))

	// 1.10000 to 1.08900 is 1100 points; each order plans just under 100.
	assert.Equal(t, qty(9090), first.units)
	assert.Equal(t, qty(9090), second.units)
}

// With no margin available a new open is refused, and the position that used it
// can still be closed.
func TestBroker_ACloseNeedsNoMargin(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(1_000_000),
		entryPrice: eurusd1_10, markPrice: eurusd1_10} // 22,000 of margin on 10,000

	other := brokerOpen("a")
	other.instrument = "GBPUSD"
	require.ErrorIs(t, br.submit(ctx, other), errInsufficientMargin)

	cl := &order{Time: testStart, accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder}
	require.NoError(t, br.submit(ctx, cl))
	// Margin is not consulted at the fill either; the bar stays above liquidation.
	require.NoError(t, br.onBar(ctx, riskBar(1, 110000, 110000, 110000, 110000)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Empty(t, act.positions)
}

// A protective stop is tested against the bar's mid even when the bar has bid
// and ask: a long's stop that the bid reached, or a short's that the ask
// reached, stays open.
//
// KNOWN DEFECT, not a safety invariant: these assertions pin unsafe behaviour and
// must be replaced by tests of the safe behaviour when #750 is fixed.
func TestBroker_KnownDefect743_AStopIgnoresTheBidAndAskItWouldTradeOn(t *testing.T) {
	captureLogs(t)
	for _, tc := range []struct {
		name string
		dir  direction
		stop int64
		bar  marketdata.Bar // mid stays 5 points inside the stop; the exit side passes it
	}{
		{"long, bid below the stop", long, 109450, quotedBar(2, 109460, 109455)},
		{"short, ask above the stop", short, 110550, quotedBar(2, 110540, 110545)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br, act := newFillBroker()
			ctx := context.Background()
			riskOpen(t, br, tc.dir, marketOrder, 110000, tc.stop)
			require.NoError(t, br.onBar(ctx, riskBar(1, 110000, 110000, 110000, 110000)))

			require.NoError(t, br.onBar(ctx, tc.bar))

			assert.Len(t, act.positions, 1, "still open: only the mid is tested")
		})
	}
}

// quotedBar is a EURUSD bar on day d with a 20 point spread: mid opens at
// 1.10000 and reaches extreme, closing at last.
func quotedBar(d int, extreme, last int64) marketdata.Bar {
	side := func(off int64) marketdata.OHLC {
		o, e, c := 110000+off, extreme+off, last+off
		return marketdata.OHLC{Open: px(o), High: px(max(o, e, c)), Low: px(min(o, e, c)), Close: px(c)}
	}
	return daily(marketdata.Bar{Instrument: "EURUSD", Time: testStart.AddDate(0, 0, d),
		Bid: side(-10), Mid: side(0), Ask: side(10)})
}
