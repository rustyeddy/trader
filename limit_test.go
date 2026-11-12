package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// submitLimit submits a limit open for account "a" on day 0 and returns it.
func submitLimit(t *testing.T, br *broker, d direction, limitN int64) *order {
	t.Helper()
	limit := px(limitN)
	o := openOrderAt("EURUSD", d, limit)
	o.orderType, o.accountID = limitOrder, "a"
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func TestBroker_OnBar_LimitNeverFillsOnTheBarThatProducedIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitLimit(t, br, long, 109000)

	// Day 0 trades through the limit, but the order was made on that bar.
	day0 := stopBar(110000, 110500, 108500, 109000)
	day0.Time = testStart
	require.NoError(t, br.onBar(context.Background(), day0))

	assert.Equal(t, stateSubmitted, o.state)
	assert.Empty(t, act.positions)
}

func TestBroker_OnBar_LongLimitWaitsThenFillsAtTheLimit(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	o := submitLimit(t, br, long, 109000)

	require.NoError(t, br.onBar(ctx, stopBar(110000, 110500, 109500, 110200)))
	assert.Equal(t, stateSubmitted, o.state, "price has not come down to the limit")
	assert.Empty(t, act.positions)

	hit := stopBar(109800, 110000, 108800, 109200)
	hit.Time = hit.AddDate(0, 0, 1)
	require.NoError(t, br.onBar(ctx, hit))

	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, px(109000), o.fillPrice, "at the limit")
	assert.Equal(t, hit.Time, o.Time)
	assert.Equal(t, px(109000), act.positions["EURUSD"].entryPrice)
	assert.Same(t, o, br.completed[0])
	require.Len(t, br.orders["EURUSD"], 1, "the filled entry queued its protective stop")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][0].reason)
}

func TestBroker_OnBar_LongLimitGappingBelowFillsAtTheBetterOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitLimit(t, br, long, 109000)

	// Opens at 1.08700 and stays above the long's stop (107910).
	require.NoError(t, br.onBar(context.Background(), stopBar(108700, 109000, 108500, 108800)))

	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, px(108700), o.fillPrice, "the open is better than the limit")
	assert.Equal(t, px(108700), act.positions["EURUSD"].entryPrice)
}

func TestBroker_OnBar_ShortLimitFillsAtTheLimitOrTheBetterOpen(t *testing.T) {
	captureLogs(t)
	tests := []struct {
		name string
		b    bar
		want price
	}{
		{"touched", stopBar(110000, 111000, 109500, 110500), px(111000)},
		{"gaps above", stopBar(111500, 111800, 111200, 111600), px(111500)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br, act := newFillBroker()
			o := submitLimit(t, br, short, 111000) // stop 112110

			require.NoError(t, br.onBar(context.Background(), tt.b))

			assert.Equal(t, stateFilled, o.state)
			assert.Equal(t, tt.want, o.fillPrice)
			assert.Equal(t, short, act.positions["EURUSD"].direction)
		})
	}
}

func TestBroker_UnfilledLimitReservesItsMargin(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	submitLimit(t, br, long, 109000)

	reserved := reservedOf(t, br, "a")
	assert.False(t, reserved.IsZero(), "a waiting limit holds margin like any submitted open")
}

func TestBroker_OnBar_MarginCloseoutCancelsAWaitingLimit(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	lim := brokerOpen("a")
	lim.orderType, lim.price, lim.state, lim.id = limitOrder, px(100000), stateSubmitted, "t-9"
	lim.Time = testStart.AddDate(0, 0, 5)
	br.orders = map[string][]*order{"EURUSD": {lim}}

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Equal(t, stateCancelled, lim.state)
	assert.Contains(t, br.completed, lim)
}

func TestBroker_OnBar_RestingLimitIsRejectedWhenEquityFellWhileItWaited(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	br.margin = func(*account) (marginStatus, error) { return marginHealthy, nil } // keep liquidation out of it
	ctx := context.Background()
	o := submitLimit(t, br, long, 109000)
	units := o.units
	require.False(t, units.IsZero())

	// While the limit waits, another position loses nearly everything:
	// -$9,900 unrealized leaves too little margin for the limit's units.
	act.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: qty(100000),
		entryPrice: px(150000), markPrice: px(140100)}

	hit := stopBar(109800, 110000, 108800, 109200)
	hit.Time = hit.AddDate(0, 0, 1)
	require.NoError(t, br.onBar(ctx, hit), "a rejection is an outcome, not a failure")

	assert.Equal(t, stateRejected, o.state)
	assert.Contains(t, o.history[len(o.history)-1].reason, "insufficient margin")
	assert.Equal(t, units, o.units, "the submitted size is never changed")
	assert.Zero(t, o.fillPrice)
	assert.NotContains(t, act.positions, "EURUSD")
	assert.Equal(t, usd(10000), act.balance)
	assert.Empty(t, br.orders["EURUSD"], "rejected: gone from the queue, and no stop was queued")
	assert.Same(t, o, br.completed[0])
}

func TestBroker_OnBar_RestingLimitStillFillsWhenMarginHolds(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitLimit(t, br, long, 109000)

	hit := stopBar(109800, 110000, 108800, 109200)
	hit.Time = hit.AddDate(0, 0, 1)
	require.NoError(t, br.onBar(context.Background(), hit))

	assert.Equal(t, stateFilled, o.state)
	assert.Contains(t, act.positions, "EURUSD")
}

func TestBroker_Submit_SecondPendingOpenOnTheInstrumentIsRejected(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	first := submitLimit(t, br, long, 109000)

	second := openOrderAt("EURUSD", long, px(108500))
	second.orderType, second.accountID = limitOrder, "a"
	err := br.submit(context.Background(), second)

	require.ErrorIs(t, err, errOpenPending)
	assert.True(t, isRejection(err), "an expected trading outcome, not a fatal accounting error")
	assert.Empty(t, second.id, "it never reached the queue")
	assert.Equal(t, []*order{first}, br.orders["EURUSD"])
}

func TestBroker_Submit_PendingOpenOnlyBlocksTheSameAccountAndInstrument(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	br.addAccount("b", usd(10000))
	submitLimit(t, br, long, 109000)
	ctx := context.Background()

	otherAccount := brokerOpen("b")
	otherInstrument := brokerOpen("a")
	otherInstrument.instrument = "GBPUSD"
	closing := &order{Time: testStart, accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder}

	require.NoError(t, br.submit(ctx, otherAccount))
	require.NoError(t, br.submit(ctx, otherInstrument))
	require.NoError(t, br.submit(ctx, closing), "only opens compete")
}

func TestBroker_Submit_OpenIsAllowedOnceThePendingOneIsGone(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	first := submitLimit(t, br, long, 109000)
	first.record(stateCancelled, first.Time, "test")
	br.complete(first)

	require.NoError(t, br.submit(context.Background(), brokerOpen("a")))
}

func TestTrader_HandleOrder_DuplicatePendingOpenIsRecordedNotFatal(t *testing.T) {
	captureLogs(t)
	ctx := context.Background()
	tr := newTestTrader()
	mk := func(limitN int64) *order {
		limit := px(limitN)
		return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: limitOrder,
			price: limit, stopLoss: px(limitN - 1100)}
	}
	first, second := mk(109000), mk(108500)

	require.NoError(t, tr.handleOrder(ctx, first))
	require.NoError(t, tr.handleOrder(ctx, second), "the run continues")

	assert.Equal(t, stateSubmitted, first.state)
	assert.Equal(t, stateRejected, second.state)
	assert.Contains(t, second.history[len(second.history)-1].reason, "already pending")
}
