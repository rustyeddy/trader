package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stopBar(oN int64, hN int64, lN int64, cN int64) bar {
	o, h, l, c := px(oN), px(hN), px(lN), px(cN)
	return daily(bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 1), ohlc: ohlc{o: o, h: h, l: l, c: c}})
}

// restingStop gives the account a long EURUSD position protected by a stop
// order the broker holds.
func restingStop(br *broker, act *account, dir direction, stopN int64) *order {
	stop := px(stopN)
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: dir, units: qty(1000),
		entryPrice: px(110000), markPrice: px(110000)}
	s := &order{id: br.nextID(), accountID: act.id, instrument: "EURUSD", direction: dir, action: close,
		orderType: stopOrder, reason: reasonStopLoss, price: stop, state: stateSubmitted, Time: testStart}
	br.orders["EURUSD"] = append(br.orders["EURUSD"], s)
	return s
}

func TestBroker_FilledOpenQueuesAProtectiveStopOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	op := submittedOrder(t, br, open, long) // stop 108900

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 110000)))

	require.Len(t, br.orders["EURUSD"], 1, "the filled open left; its stop is waiting")
	s := br.orders["EURUSD"][0]
	assert.Equal(t, stopOrder, s.orderType)
	assert.Equal(t, close, s.action)
	assert.Equal(t, reasonStopLoss, s.reason)
	assert.Equal(t, px(108900), s.price)
	assert.Equal(t, long, s.direction, "the direction of the position it protects")
	assert.Equal(t, op.accountID, s.accountID)
	assert.Equal(t, stateSubmitted, s.state)
	assert.NotEmpty(t, s.id)
}

func TestBroker_OpenWithoutAStopQueuesNoStopOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := &order{id: "t-1", accountID: "a", instrument: "EURUSD", action: open, direction: long, fillPrice: px(110000)}

	br.placeExits(o)

	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_StopOrderClosesThePositionAtTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	require.Contains(t, act.positions, "EURUSD")

	crash := daily(bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: px(109500), h: px(109600), l: px(108000), c: px(108200)}})
	require.NoError(t, br.onBar(ctx, crash))

	assert.Empty(t, act.positions, "stopped out")
	assert.Empty(t, br.orders["EURUSD"], "the filled stop left the queue")
	stops := br.completed[len(br.completed)-1]
	assert.Equal(t, reasonStopLoss, stops.reason)
	assert.Equal(t, stateFilled, stops.state)
	assert.Equal(t, px(108900), stops.fillPrice, "at the stop, not the low")
	assert.Equal(t, crash.Time, stops.Time)
	assert.Equal(t, op.units, stops.units)
	// 110000 -> 108900 is 1100 points a unit: 1.1 cents a unit, rounded down.
	assert.Equal(t, msub(usd(10000), cents((whole(op.units)*11+9)/10)), act.balance)
}

func TestBroker_OnBar_StopOrderCanTriggerOnTheEntryBar(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	submittedOrder(t, br, open, long) // stop 108900

	// Opens at 1.10000, then the same bar trades down through the stop.
	require.NoError(t, br.onBar(context.Background(), stopBar(110000, 110100, 108800, 108900)))

	assert.Empty(t, act.positions)
	assert.Equal(t, reasonStopLoss, br.completed[len(br.completed)-1].reason)
}

func TestBroker_OnBar_StopOrderIgnoresOtherInstruments(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	restingStop(br, act, long, 108900)
	other := stopBar(100000, 100000, 100000, 100000)
	other.instrument = "GBPUSD"

	require.NoError(t, br.onBar(context.Background(), other))

	assert.Contains(t, act.positions, "EURUSD")
	assert.Empty(t, br.completed)
}

func TestBroker_OnBar_StopOrderBeatsTheMarginCheck(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	restingStop(br, act, long, 108900)

	require.NoError(t, br.onBar(context.Background(), stopBar(109000, 109000, 108000, 108000)))

	require.Len(t, br.completed, 1)
	assert.Equal(t, reasonStopLoss, br.completed[0].reason, "the stop closed it before any margin closeout")
}

func TestBroker_OnBar_StrategyCloseCancelsTheRestingStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submittedOrder(t, br, open, long)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	stop := br.orders["EURUSD"][0]
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))

	require.NoError(t, br.onBar(ctx, fillBar(2, 110500)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, stateCancelled, stop.state, "no position left to protect")
	assert.Equal(t, "position closed", stop.history[len(stop.history)-1].reason)
	assert.Equal(t, fillBar(2, 0).Time, stop.Time)
	assert.Empty(t, br.orders["EURUSD"])
	assert.Empty(t, act.positions)
	n := len(br.completed)
	assert.Same(t, cl, br.completed[n-2], "the close completed first")
	assert.Same(t, stop, br.completed[n-1], "then its stop was cancelled")
}

func TestBroker_OnBar_MarginCloseoutCancelsTheRestingStop(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	stop := restingStop(br, act, long, 90000) // far below: only the closeout closes it
	act.positions["EURUSD"].units, act.positions["EURUSD"].entryPrice = qty(100000), px(111000)

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Empty(t, act.positions["EURUSD"], "liquidated")
	assert.Equal(t, stateCancelled, stop.state)
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_GapThroughTheStopFillsAtTheOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))

	// The next bar opens below the stop and never trades at it.
	gap := daily(bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: px(108000), h: px(108400), l: px(107500), c: px(108200)}})
	require.NoError(t, br.onBar(ctx, gap))

	assert.Empty(t, act.positions)
	stop := br.completed[len(br.completed)-1]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, px(108000), stop.fillPrice, "the open, not the stop price that never traded")
	// 110000 -> 108000 is 2000 points a unit: 2 cents a unit.
	assert.Equal(t, msub(usd(10000), cents(whole(op.units)*2)), act.balance)
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_EntryAlreadyBeyondTheStopIsClosedAtOnce(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900

	// The fill bar opens below the long's stop: the entry fills at that open,
	// and its stop is already beyond it.
	gap := daily(bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 1), ohlc: ohlc{o: px(108000), h: px(108400), l: px(107500), c: px(108200)}})
	require.NoError(t, br.onBar(ctx, gap))

	assert.Equal(t, stateFilled, op.state)
	assert.Equal(t, px(108000), op.fillPrice)
	assert.Empty(t, act.positions, "no position is left running beyond its stop")
	require.Len(t, br.completed, 2)
	assert.Same(t, op, br.completed[0])
	stop := br.completed[1]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, px(108000), stop.fillPrice, "closed at the same open: no further loss")
	assert.Equal(t, usd(10000), act.balance, "entered and exited at one price")
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_StopIgnoresABarOlderThanItself(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	s := restingStop(br, act, long, 108900)
	s.Time = testStart.AddDate(0, 0, 5)
	old := stopBar(108000, 108500, 107500, 108200) // day 1: trades through the stop, but before it existed

	require.NoError(t, br.onBar(context.Background(), old))

	assert.Equal(t, stateSubmitted, s.state)
	assert.Contains(t, act.positions, "EURUSD")

	same := old
	same.Time = s.Time
	require.NoError(t, br.onBar(context.Background(), same))
	assert.Equal(t, stateFilled, s.state, "the bar it was placed on is eligible")
}

func TestBroker_OnBar_OnlyTheAccountWhoseStopTriggersIsClosed(t *testing.T) {
	captureLogs(t)
	br, a := newFillBroker()
	b := br.addAccount("b", usd(10000))
	stopA := restingStop(br, a, long, 108900)
	stopB := restingStop(br, b, long, 108000)

	// Low 1.08500 reaches a's stop but not b's.
	require.NoError(t, br.onBar(context.Background(), stopBar(109500, 109600, 108500, 109000)))

	assert.Empty(t, a.positions, "a was stopped out")
	assert.Equal(t, stateFilled, stopA.state)
	assert.Contains(t, b.positions, "EURUSD", "b's position is untouched")
	assert.Equal(t, stateSubmitted, stopB.state)
	assert.Equal(t, usd(10000), b.balance)
	assert.Equal(t, []*order{stopB}, br.orders["EURUSD"])
}

func TestBroker_OnBar_ClosingOneAccountsPositionKeepsAnothersStop(t *testing.T) {
	captureLogs(t)
	br, a := newFillBroker()
	b := br.addAccount("b", usd(10000))
	stopA := restingStop(br, a, long, 108900)
	stopB := restingStop(br, b, long, 108000)
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart}
	require.NoError(t, br.submit(context.Background(), cl))

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 110500)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, stateCancelled, stopA.state, "a's stop went with a's position")
	assert.Equal(t, stateSubmitted, stopB.state, "b's stop on the same instrument stays")
	assert.Contains(t, b.positions, "EURUSD")
}

func TestBroker_OnBar_ShortGapsAboveTheStopAndClosesAtTheOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, short) // stop 111100
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	require.Equal(t, short, act.positions["EURUSD"].direction)
	stop := br.orders["EURUSD"][0]
	require.Equal(t, short, stop.direction)

	// The next bar opens above the stop and never trades at it.
	gap := daily(bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: px(112000), h: px(112500), l: px(111800), c: px(112200)}})
	require.NoError(t, br.onBar(ctx, gap))

	assert.Empty(t, act.positions)
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, px(112000), stop.fillPrice, "the open, not the stop price that never traded")
	assert.Equal(t, gap.Time, stop.Time)
	assert.Equal(t, op.units, stop.units)
	// A short from 1.10000 to 1.12000 loses 2000 points a unit: 2 cents a unit.
	assert.Equal(t, msub(usd(10000), cents(whole(op.units)*2)), act.balance)
	assert.Empty(t, br.orders["EURUSD"], "the filled stop left the queue")
	assert.Same(t, stop, br.completed[len(br.completed)-1], "and is retained as completed")
}
