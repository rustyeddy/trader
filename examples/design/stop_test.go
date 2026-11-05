package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stopBar(o, h, l, c price) bar {
	return bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 1), ohlc: ohlc{o: o, h: h, l: l, c: c}}
}

// restingStop gives the account a long EURUSD position protected by a stop
// order the broker holds.
func restingStop(br *broker, act *account, dir direction, stop price) *order {
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: dir, units: 1000,
		entryPrice: 110000, markPrice: 110000}
	s := &order{id: br.nextID(), accountID: act.id, instrument: "EURUSD", direction: dir, action: close,
		orderType: stopOrder, reason: reasonStopLoss, price: stop, state: stateSubmitted, Time: testStart}
	if br.orders == nil {
		br.orders = make(map[string][]*order)
	}
	br.orders["EURUSD"] = append(br.orders["EURUSD"], s)
	return s
}

func TestStopFill(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		stop price
		b    bar
		want price
		hit  bool
	}{
		{"long, bar stays above", long, 108900, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"long, low touches the stop", long, 108900, stopBar(110000, 110500, 108900, 110200), 108900, true},
		{"long, trades through", long, 108900, stopBar(110000, 110500, 108000, 109500), 108900, true},
		{"long, gaps below: fills at the open", long, 108900, stopBar(108000, 108500, 107500, 108200), 108000, true},
		{"short, bar stays below", short, 111100, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"short, high touches the stop", short, 111100, stopBar(110000, 111100, 109000, 110200), 111100, true},
		{"short, gaps above: fills at the open", short, 111100, stopBar(112000, 112500, 111800, 112200), 112000, true},
		{"no direction never triggers", noDirection, 108900, stopBar(1, 1, 0, 1), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, hit := stopFill(tt.dir, tt.stop, tt.b)
			assert.Equal(t, tt.hit, hit)
			assert.Equal(t, tt.want, p)
		})
	}
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
	assert.Equal(t, price(108900), s.price)
	assert.Equal(t, long, s.direction, "the direction of the position it protects")
	assert.Equal(t, op.accountID, s.accountID)
	assert.Equal(t, stateSubmitted, s.state)
	assert.NotEmpty(t, s.id)
}

func TestBroker_OpenWithoutAStopQueuesNoStopOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := &order{id: "t-1", accountID: "a", instrument: "EURUSD", action: open, direction: long, fillPrice: 110000}

	br.placeStop(o)

	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_StopOrderClosesThePositionAtTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	require.Contains(t, act.positions, "EURUSD")

	crash := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 109500, h: 109600, l: 108000, c: 108200}}
	require.NoError(t, br.onBar(ctx, crash))

	assert.Empty(t, act.positions, "stopped out")
	assert.Empty(t, br.orders["EURUSD"], "the filled stop left the queue")
	stops := br.completed[len(br.completed)-1]
	assert.Equal(t, reasonStopLoss, stops.reason)
	assert.Equal(t, stateFilled, stops.state)
	assert.Equal(t, price(108900), stops.fillPrice, "at the stop, not the low")
	assert.Equal(t, crash.Time, stops.Time)
	assert.Equal(t, op.units, stops.units)
	// 110000 -> 108900 is 1100 points a unit: 1.1 cents a unit, rounded down.
	assert.Equal(t, usd(10000)-money((op.units*11+9)/10), act.balance)
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
	act.positions["EURUSD"].units, act.positions["EURUSD"].entryPrice = 100000, 111000

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
	gap := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 108000, h: 108400, l: 107500, c: 108200}}
	require.NoError(t, br.onBar(ctx, gap))

	assert.Empty(t, act.positions)
	stop := br.completed[len(br.completed)-1]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(108000), stop.fillPrice, "the open, not the stop price that never traded")
	// 110000 -> 108000 is 2000 points a unit: 2 cents a unit.
	assert.Equal(t, usd(10000)-money(op.units*2), act.balance)
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_EntryAlreadyBeyondTheStopIsClosedAtOnce(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900

	// The fill bar opens below the long's stop: the entry fills at that open,
	// and its stop is already beyond it.
	gap := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 1), ohlc: ohlc{o: 108000, h: 108400, l: 107500, c: 108200}}
	require.NoError(t, br.onBar(ctx, gap))

	assert.Equal(t, stateFilled, op.state)
	assert.Equal(t, price(108000), op.fillPrice)
	assert.Empty(t, act.positions, "no position is left running beyond its stop")
	require.Len(t, br.completed, 2)
	assert.Same(t, op, br.completed[0])
	stop := br.completed[1]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(108000), stop.fillPrice, "closed at the same open: no further loss")
	assert.Equal(t, usd(10000), act.balance, "entered and exited at one price")
	assert.Empty(t, br.orders["EURUSD"])
}
