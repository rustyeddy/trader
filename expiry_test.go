package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(d int) time.Time { return testStart.AddDate(0, 0, d) }

// limitUntil submits a long limit at 1.09000 that expires at day exp.
func limitUntil(t *testing.T, br *broker, exp int) *order {
	t.Helper()
	o := openOrderAt("EURUSD", long, px(109000))
	o.orderType, o.accountID, o.expires = limitOrder, "a", day(exp)
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

// dayBar is a EURUSD bar on day d with the given range.
func dayBar(d int, oN int64, hN int64, lN int64, cN int64) bar {
	o, h, l, c := px(oN), px(hN), px(lN), px(cN)
	return daily(bar{instrument: "EURUSD", Time: day(d), ohlc: ohlc{o: o, h: h, l: l, c: c}})
}

func TestBroker_OnBar_OrderIsLiveBeforeItsExpiry(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := limitUntil(t, br, 3)

	require.NoError(t, br.onBar(context.Background(), dayBar(2, 109500, 109600, 108800, 109200)))

	assert.Equal(t, stateFilled, o.state, "the day before it expires")
	assert.Contains(t, act.positions, "EURUSD")
}

func TestBroker_OnBar_ExpiryBarCancelsInsteadOfFilling(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	o := limitUntil(t, br, 3)
	require.NoError(t, br.onBar(ctx, dayBar(1, 110000, 110500, 109500, 110200)))
	require.Equal(t, stateSubmitted, o.state)

	// The expiry bar trades through the limit, but the order is no longer live.
	require.NoError(t, br.onBar(ctx, dayBar(3, 109500, 109600, 108800, 109200)))

	assert.Equal(t, stateCancelled, o.state)
	last := o.history[len(o.history)-1]
	assert.Equal(t, "expired", last.reason)
	assert.Equal(t, day(3), last.time, "stamped with the bar that expired it")
	assert.Empty(t, act.positions)
	assert.Empty(t, br.orders["EURUSD"])
	assert.Same(t, o, br.completed[0])
}

func TestBroker_OnBar_ExpiredOrderReleasesItsMarginAndThePendingSlot(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	limitUntil(t, br, 2)
	reserved := reservedOf(t, br, "a")
	require.False(t, reserved.IsZero())

	require.NoError(t, br.onBar(context.Background(), dayBar(2, 110000, 110500, 109500, 110200)))

	reserved = reservedOf(t, br, "a")
	assert.True(t, reserved.IsZero())
	again := openOrderAt("EURUSD", long, px(109000))
	again.orderType, again.accountID, again.Time = limitOrder, "a", day(2)
	require.NoError(t, br.submit(context.Background(), again), "a new open may now be pending")
}

func TestBroker_OnBar_NoExpiryMeansGoodUntilCancelled(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := submitLimit(t, br, long, 109000)

	require.NoError(t, br.onBar(context.Background(), dayBar(400, 110000, 110500, 109500, 110200)))

	assert.Equal(t, stateSubmitted, o.state)
}

func TestBroker_OnBar_ExpiryIsOnlyCheckedForTheBarsInstrument(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := limitUntil(t, br, 2)
	other := dayBar(5, 1, 1, 1, 1)
	other.instrument = "GBPUSD"

	require.NoError(t, br.onBar(context.Background(), other))

	assert.Equal(t, stateSubmitted, o.state, "a bar for another instrument says nothing about this one")
}

func TestOrder_Validate_Expiry(t *testing.T) {
	base := func() order {
		return order{Time: testStart, instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, price: px(110000), stopLoss: px(108900)}
	}
	ok := base()
	ok.expires = day(1)
	require.NoError(t, ok.validate())

	for name, exp := range map[string]time.Time{"at its own time": testStart, "before it": day(-1)} {
		o := base()
		o.expires = exp
		require.ErrorIs(t, o.validate(), errInvalidOrder, name)
	}
	cl := order{Time: testStart, instrument: "EUR/USD", action: close, orderType: marketOrder, expires: day(1)}
	require.ErrorIs(t, cl.validate(), errInvalidOrder, "only an open expires")
}

func TestBroker_Cancel_WithdrawsAWaitingOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := submitLimit(t, br, long, 109000)

	require.NoError(t, br.cancel(context.Background(), "a", o.id, day(2)))

	assert.Equal(t, stateCancelled, o.state)
	last := o.history[len(o.history)-1]
	assert.Equal(t, "cancelled by request", last.reason)
	assert.Equal(t, day(2), last.time)
	assert.Empty(t, br.orders["EURUSD"])
	assert.Same(t, o, br.completed[0])
	reserved := reservedOf(t, br, "a")
	assert.True(t, reserved.IsZero(), "its margin is released")
}

func TestBroker_Cancel_CancelledOrderNeverFills(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitLimit(t, br, long, 109000)
	require.NoError(t, br.cancel(context.Background(), "a", o.id, day(0)))

	require.NoError(t, br.onBar(context.Background(), dayBar(1, 109500, 109600, 108800, 109200)))

	assert.Empty(t, act.positions)
}

func TestBroker_Cancel_NotWaitingIsAnExpectedRejection(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	br.addAccount("b", usd(10000))
	ctx := context.Background()
	waiting := submitLimit(t, br, long, 109000)

	t.Run("unknown id", func(t *testing.T) {
		err := br.cancel(ctx, "a", "nope", day(1))
		require.ErrorIs(t, err, errNoOrder)
		assert.True(t, isRejection(err))
	})
	t.Run("another account's order", func(t *testing.T) {
		require.ErrorIs(t, br.cancel(ctx, "b", waiting.id, day(1)), errNoOrder)
		assert.Equal(t, stateSubmitted, waiting.state, "left untouched")
	})
	t.Run("already cancelled", func(t *testing.T) {
		require.NoError(t, br.cancel(ctx, "a", waiting.id, day(1)))
		require.ErrorIs(t, br.cancel(ctx, "a", waiting.id, day(1)), errNoOrder)
	})
}

func TestBroker_Cancel_AlreadyFilledIsNoOrder(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	ctx := context.Background()
	o := submittedOrder(t, br, open, long)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	require.Equal(t, stateFilled, o.state)

	require.ErrorIs(t, br.cancel(ctx, "a", o.id, day(1)), errNoOrder)
	assert.Equal(t, stateFilled, o.state)
}

func TestBroker_Cancel_ProtectiveStopLeavesThePositionOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submittedOrder(t, br, open, long)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	stop := br.orders["EURUSD"][0]

	require.NoError(t, br.cancel(ctx, "a", stop.id, day(1)))

	assert.Equal(t, stateCancelled, stop.state)
	assert.Contains(t, act.positions, "EURUSD", "cancelling an exit does not close the position")
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_Cancel_StoppedContextIsAnError(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	o := submitLimit(t, br, long, 109000)
	ctx, stop := context.WithCancel(context.Background())
	stop()

	err := br.cancel(ctx, "a", o.id, day(1))

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, isRejection(err))
	assert.Equal(t, stateSubmitted, o.state)
}
