package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trailingEntry fills a market open at 1.10000 on day 1 with a 1% initial
// stop and the given trail, and returns the open and its resting stop.
func trailingEntry(t *testing.T, br *broker, d direction, trail price) (op, stop *order) {
	t.Helper()
	op = openOrderAt("EURUSD", d, eurusd1_10)
	op.accountID, op.trail = "a", trail
	require.NoError(t, br.submit(context.Background(), op))
	require.NoError(t, br.onBar(context.Background(), dayBar(1, 110000, 110000, 110000, 110000)))
	require.Equal(t, stateFilled, op.state)
	require.Len(t, br.orders["EURUSD"], 1)
	stop = br.orders["EURUSD"][0]
	require.Equal(t, reasonStopLoss, stop.reason)
	require.Equal(t, trail, stop.trail, "the stop carries the trail")
	return op, stop
}

func TestBroker_OnBar_TrailingStopRatchetsUpAndNeverBack(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op, stop := trailingEntry(t, br, long, 1500) // stop 108900: 1.10000 - 1500 = 1.08500 is lower, so no move yet
	require.Equal(t, price(108900), stop.price)

	// A rally: the stop follows the high up to 1.12000 - 0.01500.
	require.NoError(t, br.onBar(ctx, dayBar(2, 110500, 112000, 110200, 111500)))
	assert.Equal(t, price(110500), stop.price)
	assert.Equal(t, stateSubmitted, stop.state)
	last := stop.history[len(stop.history)-1]
	assert.Equal(t, "trailed to 110500", last.reason)
	assert.Equal(t, day(2), last.time)

	// A lower high: the stop stays where it is.
	require.NoError(t, br.onBar(ctx, dayBar(3, 111000, 111500, 110600, 111000)))
	assert.Equal(t, price(110500), stop.price)
	assert.Contains(t, act.positions, "EURUSD")

	// The pullback reaches the trailed stop: out at 1.10500, in profit.
	require.NoError(t, br.onBar(ctx, dayBar(4, 110800, 110900, 109000, 109500)))
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(110500), stop.fillPrice)
	assert.Empty(t, act.positions)
	// 1.10000 -> 1.10500 is 500 points a unit: half a cent a unit.
	assert.Equal(t, usd(10000)+money(op.units/2), act.balance)
}

func TestBroker_OnBar_ABarCannotBothRaiseAStopAndHitIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	_, stop := trailingEntry(t, br, long, 1500)

	// High 1.13000 would trail the stop to 1.11500, and the low 1.10000 is
	// below that. The stop is checked at its old level (1.08900) first.
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110500, 113000, 110000, 112000)))

	assert.Equal(t, stateSubmitted, stop.state, "not stopped out on the bar that raised it")
	assert.Equal(t, price(111500), stop.price, "but the level has moved for the next bar")
	assert.Contains(t, act.positions, "EURUSD")

	require.NoError(t, br.onBar(context.Background(), dayBar(3, 112000, 112200, 111400, 111600)))
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(111500), stop.fillPrice)
}

func TestBroker_OnBar_TrailedStopGapsLikeAnyStop(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	_, stop := trailingEntry(t, br, long, 1500)
	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110500, 112000, 110200, 111500)))
	require.Equal(t, price(110500), stop.price)

	require.NoError(t, br.onBar(context.Background(), dayBar(3, 109000, 109500, 108500, 109200)))

	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(109000), stop.fillPrice, "the open, below the trailed level")
}

func TestBroker_OnBar_TrailingStopOnAShortRatchetsDown(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	op, stop := trailingEntry(t, br, short, 1500) // stop 111100
	ctx := context.Background()

	require.NoError(t, br.onBar(ctx, dayBar(2, 109500, 109800, 108000, 108500)))
	assert.Equal(t, price(109500), stop.price, "the low plus the trail")

	require.NoError(t, br.onBar(ctx, dayBar(3, 108800, 109000, 108200, 108900)))
	assert.Equal(t, price(109500), stop.price, "a higher low does not move it back up")

	require.NoError(t, br.onBar(ctx, dayBar(4, 109000, 109700, 108900, 109600)))
	assert.Equal(t, stateFilled, stop.state)
	assert.Equal(t, price(109500), stop.fillPrice)
	assert.Empty(t, act.positions)
	assert.Equal(t, usd(10000)+money(op.units/2), act.balance)
}

func TestBroker_OnBar_FixedStopNeverMoves(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	_, stop := trailingEntry(t, br, long, 0)
	n := len(stop.history)

	require.NoError(t, br.onBar(context.Background(), dayBar(2, 112000, 115000, 111500, 114000)))

	assert.Equal(t, price(108900), stop.price)
	assert.Len(t, stop.history, n, "no trail event")
}

func TestBroker_OnBar_TrailedStopIsCancelledWithThePosition(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	_, stop := trailingEntry(t, br, long, 1500)
	require.NoError(t, br.onBar(ctx, dayBar(2, 110500, 112000, 110200, 111500)))
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: day(2)}
	require.NoError(t, br.submit(ctx, cl))

	require.NoError(t, br.onBar(ctx, dayBar(3, 111800, 112000, 111600, 111900)))

	assert.Equal(t, stateFilled, cl.state)
	assert.Equal(t, stateCancelled, stop.state)
	assert.Empty(t, act.positions)
	assert.Empty(t, br.orders["EURUSD"])
}

func TestOrder_Validate_Trail(t *testing.T) {
	ok := order{Time: testStart, instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, price: 110000, stopLoss: 108900, trail: 1000}
	require.NoError(t, ok.validate())

	neg := ok
	neg.trail = -1
	require.ErrorIs(t, neg.validate(), errInvalidOrder)

	onClose := order{Time: testStart, instrument: "EUR/USD", action: close, orderType: marketOrder, trail: 1000}
	require.ErrorIs(t, onClose.validate(), errInvalidOrder, "only an open carries a trail")
}
