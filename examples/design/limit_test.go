package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// submitLimit submits a limit open for account "a" on day 0 and returns it.
func submitLimit(t *testing.T, br *broker, d direction, limit price) *order {
	t.Helper()
	o := openOrderAt("EURUSD", d, limit)
	o.orderType, o.accountID = limitOrder, "a"
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func TestThroughPrice(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		p    price
		b    bar
		want price
		hit  bool
	}{
		{"long, bar stays above", long, 109000, stopBar(110000, 110500, 109500, 110200), 0, false},
		{"long, low touches", long, 109000, stopBar(110000, 110500, 109000, 110200), 109000, true},
		{"long, gap below fills at the open", long, 109000, stopBar(108000, 108500, 107500, 108200), 108000, true},
		{"short, bar stays below", short, 111000, stopBar(110000, 110500, 109500, 110200), 0, false},
		{"short, high touches", short, 111000, stopBar(110000, 111000, 109500, 110200), 111000, true},
		{"short, gap above fills at the open", short, 111000, stopBar(112000, 112500, 111800, 112200), 112000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, hit := throughPrice(tt.dir, tt.p, tt.b)
			assert.Equal(t, tt.hit, hit)
			assert.Equal(t, tt.want, p)
		})
	}
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
	hit.Time = hit.Time.AddDate(0, 0, 1)
	require.NoError(t, br.onBar(ctx, hit))

	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, price(109000), o.fillPrice, "at the limit")
	assert.Equal(t, hit.Time, o.Time)
	assert.Equal(t, price(109000), act.positions["EURUSD"].entryPrice)
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
	assert.Equal(t, price(108700), o.fillPrice, "the open is better than the limit")
	assert.Equal(t, price(108700), act.positions["EURUSD"].entryPrice)
}

func TestBroker_OnBar_ShortLimitFillsAtTheLimitOrTheBetterOpen(t *testing.T) {
	captureLogs(t)
	tests := []struct {
		name string
		b    bar
		want price
	}{
		{"touched", stopBar(110000, 111000, 109500, 110500), 111000},
		{"gaps above", stopBar(111500, 111800, 111200, 111600), 111500},
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

	reserved, err := br.reservedMargin("a")

	require.NoError(t, err)
	assert.Positive(t, reserved, "a waiting limit holds margin like any submitted open")
}

func TestBroker_OnBar_MarginCloseoutCancelsAWaitingLimit(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	lim := brokerOpen("a")
	lim.orderType, lim.price, lim.state, lim.id = limitOrder, 100000, stateSubmitted, "t-9"
	lim.Time = testStart.AddDate(0, 0, 5)
	br.orders = map[string][]*order{"EURUSD": {lim}}

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Equal(t, stateCancelled, lim.state)
	assert.Contains(t, br.completed, lim)
}
