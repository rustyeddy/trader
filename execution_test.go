package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOrder_Execution is the one table for which side of a bar triggers a
// resting order and what it fills at. Every row is a submitted EURUSD order
// made on day 0 seeing the day-1 bar.
func TestOrder_Execution(t *testing.T) {
	type row struct {
		name string
		typ  orderType
		act  action
		dir  direction
		p    price
		b    bar
		want price
		hit  bool
	}
	rows := []row{
		// Buy limit (long entry) and sell stop (long's protective stop) wait for a fall.
		{"long limit entry, bar stays above", limitOrder, open, long, 109000, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"long limit entry, low touches", limitOrder, open, long, 109000, stopBar(110000, 110400, 109000, 110200), 109000, true},
		{"long limit entry, gap below fills at the better open", limitOrder, open, long, 109000, stopBar(108000, 108500, 107500, 108200), 108000, true},
		{"long stop exit, bar stays above", stopOrder, close, long, 108900, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"long stop exit, low touches", stopOrder, close, long, 108900, stopBar(110000, 110500, 108900, 110200), 108900, true},
		{"long stop exit, trades through", stopOrder, close, long, 108900, stopBar(110000, 110500, 108000, 109500), 108900, true},
		{"long stop exit, gap below fills at the worse open", stopOrder, close, long, 108900, stopBar(108000, 108500, 107500, 108200), 108000, true},
		// Sell limit (long's take profit) and buy stop (long entry) wait for a rise.
		{"long take profit, bar stays below", limitOrder, close, long, 112000, stopBar(110000, 111900, 109500, 110200), 0, false},
		{"long take profit, high touches", limitOrder, close, long, 112000, stopBar(110000, 112000, 109500, 110200), 112000, true},
		{"long take profit, gap above fills at the better open", limitOrder, close, long, 112000, stopBar(112500, 113000, 112200, 112800), 112500, true},
		{"long stop entry, bar stays below", stopOrder, open, long, 110500, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"long stop entry, high touches", stopOrder, open, long, 110500, stopBar(110000, 110500, 109500, 110200), 110500, true},
		{"long stop entry, gap above fills at the worse open", stopOrder, open, long, 110500, stopBar(111000, 111500, 110800, 111200), 111000, true},
		// Shorts mirror.
		{"short limit entry, high touches", limitOrder, open, short, 111000, stopBar(110000, 111000, 109500, 110500), 111000, true},
		{"short limit entry, gap above fills at the better open", limitOrder, open, short, 111000, stopBar(111500, 111800, 111200, 111600), 111500, true},
		{"short stop exit, bar stays below", stopOrder, close, short, 111100, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"short stop exit, high touches", stopOrder, close, short, 111100, stopBar(110000, 111100, 109000, 110200), 111100, true},
		{"short stop exit, gap above fills at the worse open", stopOrder, close, short, 111100, stopBar(112000, 112500, 111800, 112200), 112000, true},
		{"short take profit, bar stays above", limitOrder, close, short, 108000, stopBar(110000, 110400, 108100, 110200), 0, false},
		{"short take profit, gap below fills at the better open", limitOrder, close, short, 108000, stopBar(107500, 107800, 107000, 107600), 107500, true},
		{"short stop entry, bar stays above", stopOrder, open, short, 109000, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"short stop entry, low touches", stopOrder, open, short, 109000, stopBar(110000, 110400, 109000, 110200), 109000, true},
		{"short stop entry, gap below fills at the worse open", stopOrder, open, short, 109000, stopBar(108000, 108500, 107500, 108200), 108000, true},
		// Market orders fill at the open, whatever the range.
		{"market open", marketOrder, open, long, 0, stopBar(110000, 110400, 109500, 110200), 110000, true},
		{"market close", marketOrder, close, noDirection, 0, stopBar(110000, 110400, 109500, 110200), 110000, true},
		// Unsupported types never execute, even when the bar crosses their price.
		{"missing order type", noOrder, open, long, 110000, stopBar(110000, 110400, 109500, 110200), 0, false},
		{"unknown order type", orderType(99), open, long, 110000, stopBar(110000, 110400, 109500, 110200), 0, false},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			o := &order{instrument: "EURUSD", orderType: tt.typ, action: tt.act, direction: tt.dir,
				price: tt.p, state: stateSubmitted, Time: testStart}
			p, hit := o.execution(tt.b)
			assert.Equal(t, tt.hit, hit)
			assert.Equal(t, tt.want, p)
		})
	}
}

func TestOrder_Execution_StateInstrumentAndTime(t *testing.T) {
	hit := stopBar(110000, 110500, 108000, 109500) // day 1, through 108900 and 110500
	mk := func() *order {
		return &order{instrument: "EURUSD", orderType: stopOrder, action: close, direction: long,
			reason: reasonStopLoss, price: 108900, state: stateSubmitted, Time: testStart}
	}

	o := mk()
	o.state = stateFilled
	_, ok := o.execution(hit)
	assert.False(t, ok, "only a submitted order executes")

	o = mk()
	o.instrument = "GBPUSD"
	_, ok = o.execution(hit)
	assert.False(t, ok, "another instrument's bar says nothing")

	o = mk()
	o.Time = hit.AddDate(0, 0, 1)
	_, ok = o.execution(hit)
	assert.False(t, ok, "never against a bar older than the order")

	o = mk()
	o.Time = hit.Time
	_, ok = o.execution(hit)
	assert.True(t, ok, "an exit may fill on the bar it was placed on")

	entry := mk()
	entry.reason, entry.action, entry.orderType, entry.price, entry.Time = reasonStrategy, open, limitOrder, 109000, hit.Time
	_, ok = entry.execution(hit)
	assert.False(t, ok, "a strategy's order never fills on the bar that produced it")

	market := mk()
	market.reason, market.orderType, market.Time = reasonStrategy, marketOrder, hit.Time
	_, ok = market.execution(hit)
	assert.False(t, ok, "a strategy's market close included")
}
