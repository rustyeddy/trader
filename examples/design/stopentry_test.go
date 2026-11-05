package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// submitStopEntry submits a stop entry for account "a" on day 0 and returns it.
func submitStopEntry(t *testing.T, br *broker, d direction, trigger price) *order {
	t.Helper()
	o := openOrderAt("EURUSD", d, trigger)
	o.orderType, o.accountID = stopOrder, "a"
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func TestOrder_Trigger(t *testing.T) {
	tests := []struct {
		name string
		o    order
		b    bar
		want price
		hit  bool
	}{
		// A buy stop waits for the price to rise to it.
		{"long stop entry, bar stays below", order{orderType: stopOrder, action: open, direction: long, price: 110500}, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"long stop entry, high touches", order{orderType: stopOrder, action: open, direction: long, price: 110500}, stopBar(110000, 110500, 109500, 110200), 110500, true},
		{"long stop entry, gap above fills at the worse open", order{orderType: stopOrder, action: open, direction: long, price: 110500}, stopBar(111000, 111500, 110800, 111200), 111000, true},
		// A sell stop waits for it to fall.
		{"short stop entry, bar stays above", order{orderType: stopOrder, action: open, direction: short, price: 109000}, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"short stop entry, low touches", order{orderType: stopOrder, action: open, direction: short, price: 109000}, stopBar(110000, 110400, 109000, 110200), 109000, true},
		{"short stop entry, gap below fills at the worse open", order{orderType: stopOrder, action: open, direction: short, price: 109000}, stopBar(108000, 108500, 107500, 108200), 108000, true},
		// The same two directions as a limit entry trigger the other way.
		{"long limit entry waits for a fall", order{orderType: limitOrder, action: open, direction: long, price: 109000}, stopBar(110000, 110400, 109500, 110200), 0, false},
		// A take profit is a limit close: it triggers the way the position profits.
		{"long's take profit waits for a rise", order{orderType: limitOrder, action: close, direction: long, price: 112000}, stopBar(110000, 111900, 109500, 110200), 0, false},
		{"long's take profit fills at the target", order{orderType: limitOrder, action: close, direction: long, price: 112000}, stopBar(110000, 112000, 109500, 110200), 112000, true},
		{"long's take profit gap above fills at the better open", order{orderType: limitOrder, action: close, direction: long, price: 112000}, stopBar(112500, 113000, 112200, 112800), 112500, true},
		{"short's take profit waits for a fall", order{orderType: limitOrder, action: close, direction: short, price: 108000}, stopBar(110000, 110400, 108100, 110200), 0, false},
		{"short's take profit gap below fills at the better open", order{orderType: limitOrder, action: close, direction: short, price: 108000}, stopBar(107500, 107800, 107000, 107600), 107500, true},
		{"long's protective stop waits for a fall", order{orderType: stopOrder, action: close, direction: long, price: 108900}, stopBar(110000, 110400, 108800, 110200), 108900, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, hit := tt.o.trigger(tt.b)
			assert.Equal(t, tt.hit, hit)
			assert.Equal(t, tt.want, p)
		})
	}
}

func TestOrder_Phase(t *testing.T) {
	assert.Equal(t, 0, (&order{orderType: marketOrder}).phase())
	assert.Equal(t, 1, (&order{orderType: limitOrder, action: open}).phase())
	assert.Equal(t, 2, (&order{orderType: stopOrder, action: open}).phase(), "stop entry")
	assert.Equal(t, 3, (&order{orderType: stopOrder, action: close}).phase(), "protective stop")
	assert.Equal(t, 4, (&order{orderType: limitOrder, action: close}).phase(), "take profit")
}

func TestBroker_OnBar_StopEntryNeverFillsOnTheBarThatProducedIt(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitStopEntry(t, br, long, 110500)

	day0 := stopBar(110000, 111000, 109500, 110800)
	day0.Time = testStart
	require.NoError(t, br.onBar(context.Background(), day0))

	assert.Equal(t, stateSubmitted, o.state)
	assert.Empty(t, act.positions)
}

func TestBroker_OnBar_LongStopEntryWaitsThenFillsAtTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	o := submitStopEntry(t, br, long, 110500) // protective stop 109395

	require.NoError(t, br.onBar(ctx, stopBar(110000, 110400, 109800, 110200)))
	assert.Equal(t, stateSubmitted, o.state, "price has not risen to the stop")

	hit := stopBar(110200, 110600, 109800, 110550)
	hit.Time = hit.Time.AddDate(0, 0, 1)
	require.NoError(t, br.onBar(ctx, hit))

	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, price(110500), o.fillPrice, "at the stop")
	assert.Equal(t, long, act.positions["EURUSD"].direction)
	require.Len(t, br.orders["EURUSD"], 1, "the entry queued its protective stop")
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][0].reason)
}

func TestBroker_OnBar_StopEntryGappingAboveFillsAtTheWorseOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitStopEntry(t, br, long, 110500)

	require.NoError(t, br.onBar(context.Background(), stopBar(111000, 111500, 110800, 111200)))

	assert.Equal(t, price(111000), o.fillPrice, "buying into the move costs the gap")
	assert.Equal(t, price(111000), act.positions["EURUSD"].entryPrice)
}

func TestBroker_OnBar_ShortStopEntryFillsOnAFall(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitStopEntry(t, br, short, 109000) // protective stop 110090

	require.NoError(t, br.onBar(context.Background(), stopBar(109800, 109900, 108900, 109000)))

	assert.Equal(t, stateFilled, o.state)
	assert.Equal(t, price(109000), o.fillPrice)
	assert.Equal(t, short, act.positions["EURUSD"].direction)
}

func TestBroker_OnBar_StopEntryProtectiveStopCanTriggerOnTheSameBar(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitStopEntry(t, br, long, 110500) // protective stop 109395

	// Rises through the entry, then falls through the protective stop.
	require.NoError(t, br.onBar(context.Background(), stopBar(110000, 111000, 109000, 109200)))

	assert.Equal(t, stateFilled, o.state)
	assert.Empty(t, act.positions)
	require.Len(t, br.completed, 2)
	assert.Same(t, o, br.completed[0])
	assert.Equal(t, reasonStopLoss, br.completed[1].reason)
}

func TestBroker_OnBar_StopEntryIsRejectedWhenMarginFellWhileItWaited(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	o := submitStopEntry(t, br, long, 110500)
	act.balance = usd(1) // equity collapses while the order waits

	hit := stopBar(110200, 110600, 109800, 110550)
	require.NoError(t, br.onBar(context.Background(), hit))

	assert.Equal(t, stateRejected, o.state)
	assert.Empty(t, act.positions)
}

func TestBroker_Submit_StopEntryCompetesWithOtherPendingOpens(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	submitLimit(t, br, long, 109000)

	second := openOrderAt("EURUSD", long, 110500)
	second.orderType, second.accountID = stopOrder, "a"

	require.ErrorIs(t, br.submit(context.Background(), second), errOpenPending)
}
