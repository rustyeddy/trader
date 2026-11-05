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

func TestStopFill(t *testing.T) {
	longPos := &position{instrument: "EURUSD", direction: long, stopLoss: 108900}
	shortPos := &position{instrument: "EURUSD", direction: short, stopLoss: 111100}
	tests := []struct {
		name string
		pos  *position
		b    bar
		want price
		hit  bool
	}{
		{"long, bar stays above", longPos, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"long, low touches the stop", longPos, stopBar(110000, 110500, 108900, 110200), 108900, true},
		{"long, trades through", longPos, stopBar(110000, 110500, 108000, 109500), 108900, true},
		{"long, gaps below: fills at the open", longPos, stopBar(108000, 108500, 107500, 108200), 108000, true},
		{"short, bar stays below", shortPos, stopBar(110000, 110500, 109000, 110200), 0, false},
		{"short, high touches the stop", shortPos, stopBar(110000, 111100, 109000, 110200), 111100, true},
		{"short, gaps above: fills at the open", shortPos, stopBar(112000, 112500, 111800, 112200), 112000, true},
		{"no stop never triggers", &position{direction: long}, stopBar(1, 1, 0, 1), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, hit := stopFill(tt.pos, tt.b)
			assert.Equal(t, tt.hit, hit)
			assert.Equal(t, tt.want, p)
		})
	}
}

func TestPosition_KeepsTheOpeningOrdersStop(t *testing.T) {
	act := newAccount("a", usd(10000))
	o := openOrderAt("EURUSD", long, eurusd1_10)
	o.fillPrice = eurusd1_10

	require.NoError(t, act.applyFill(o))

	assert.Equal(t, o.stopLoss, act.positions["EURUSD"].stopLoss)
}

func TestBroker_OnBar_StopLossClosesThePositionAtTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submittedOrder(t, br, open, long) // stop 108900
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	require.Contains(t, act.positions, "EURUSD")

	crash := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 109500, h: 109600, l: 108000, c: 108200}}
	require.NoError(t, br.onBar(ctx, crash))

	assert.Empty(t, act.positions, "stopped out")
	stops := br.completed[len(br.completed)-1]
	assert.Equal(t, reasonStopLoss, stops.reason)
	assert.Equal(t, close, stops.action)
	assert.Equal(t, stateFilled, stops.state)
	assert.Equal(t, price(108900), stops.fillPrice, "at the stop, not the low")
	assert.Equal(t, crash.Time, stops.Time)
	assert.Equal(t, op.units, stops.units)
	// 110000 -> 108900 is 1100 points a unit: 1.1 cents a unit, rounded down.
	assert.Equal(t, usd(10000)-money((op.units*11+9)/10), act.balance)
}

func TestBroker_OnBar_StopLossLeavesOtherInstrumentsAlone(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1000,
		entryPrice: 110000, stopLoss: 108900, markPrice: 110000}
	other := stopBar(100000, 100000, 100000, 100000)
	other.instrument = "GBPUSD"

	require.NoError(t, br.onBar(context.Background(), other))

	assert.Contains(t, act.positions, "EURUSD")
	assert.Empty(t, br.completed)
}

func TestBroker_OnBar_StopLossBeatsTheMarginCheck(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1000,
		entryPrice: 110000, stopLoss: 108900, markPrice: 110000}

	require.NoError(t, br.onBar(context.Background(), stopBar(109000, 109000, 108000, 108000)))

	require.Len(t, br.completed, 1)
	assert.Equal(t, reasonStopLoss, br.completed[0].reason, "the stop closed it before any margin closeout")
}
