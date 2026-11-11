package trader

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAction_String(t *testing.T) {
	tests := map[action]string{
		noAction:   "none",
		open:       "open",
		add:        "add",
		close:      "close",
		reduce:     "reduce",
		action(99): "unknown",
	}
	for a, want := range tests {
		assert.Equal(t, want, a.String())
	}
}

func TestDirection_String(t *testing.T) {
	tests := map[direction]string{
		noDirection:   "none",
		long:          "long",
		short:         "short",
		direction(99): "none",
	}
	for d, want := range tests {
		assert.Equal(t, want, d.String())
	}
}

func TestOrderType_String(t *testing.T) {
	tests := map[orderType]string{
		noOrder:       "none",
		marketOrder:   "market",
		limitOrder:    "limit",
		stopOrder:     "stop",
		orderType(99): "unknown",
	}
	for ot, want := range tests {
		assert.Equal(t, want, ot.String())
	}
}

func TestOrderState_String(t *testing.T) {
	tests := map[orderState]string{
		stateRequested: "requested",
		stateSized:     "sized",
		stateSubmitted: "submitted",
		stateFilled:    "filled",
		stateRejected:  "rejected",
		stateCancelled: "cancelled",
		orderState(99): "unknown",
	}
	for s, want := range tests {
		assert.Equal(t, want, s.String())
	}
}

func TestOrder_EnumValuesAreStable(t *testing.T) {
	assert.Equal(t, direction(0), noDirection)
	assert.Equal(t, direction(1), long)
	assert.Equal(t, direction(2), short)
	assert.Equal(t, action(0), noAction)
	assert.Equal(t, orderType(0), noOrder)
	assert.Equal(t, orderState(0), stateRequested, "a new order starts requested")
}

func TestOrder_ZeroValueIsNotAValidRequest(t *testing.T) {
	// There is no hold order: "do nothing" is a nil *order, so a zero-value
	// order is an unset action, not a no-op.
	require.ErrorIs(t, (&order{Time: testStart}).validate(), errInvalidOrder)
	require.ErrorIs(t, (&order{Time: testStart, instrument: "EUR/USD"}).validate(), errInvalidOrder)
}

func TestOrder_Validate(t *testing.T) {
	tests := []struct {
		name    string
		in      order
		wantErr bool
	}{
		// valid
		{"open long", order{instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}, false},
		{"open short", order{instrument: "EUR/USD", direction: short, action: open, orderType: marketOrder, price: 110000, stopLoss: 111100}, false},
		{"close without direction", order{instrument: "EUR/USD", action: close, orderType: marketOrder}, false},

		// limit orders
		{"limit open", order{instrument: "EUR/USD", direction: long, action: open, orderType: limitOrder, price: 109000, stopLoss: 107900}, false},
		{"limit without a price", order{instrument: "EUR/USD", direction: long, action: open, orderType: limitOrder, stopLoss: 107900}, true},
		{"stop entry", order{instrument: "EUR/USD", direction: long, action: open, orderType: stopOrder, price: 110500, stopLoss: 109400}, false},
		{"stop entry without a price", order{instrument: "EUR/USD", direction: long, action: open, orderType: stopOrder, stopLoss: 109400}, true},
		{"stop close is not a strategy order", order{instrument: "EUR/USD", action: close, orderType: stopOrder, price: 108900}, true},
		{"limit close is not supported", order{instrument: "EUR/USD", action: close, orderType: limitOrder, price: 109000}, true},

		// no action
		{"zero value", order{}, true},
		{"missing action", order{instrument: "EUR/USD", orderType: marketOrder}, true},

		// missing instrument on every action
		{"open missing instrument", order{direction: long, action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}, true},
		{"add missing instrument", order{action: add, orderType: marketOrder}, true},
		{"reduce missing instrument", order{action: reduce, orderType: marketOrder}, true},
		{"close missing instrument", order{action: close, orderType: marketOrder}, true},

		// invalid encodings
		{"missing order type", order{instrument: "EUR/USD", direction: long, action: open, price: 110000, stopLoss: 108900}, true},
		{"unknown order type", order{instrument: "EUR/USD", direction: long, action: open, orderType: orderType(99), price: 110000, stopLoss: 108900}, true},
		{"unknown action", order{instrument: "EUR/USD", action: action(99), orderType: marketOrder}, true},
		{"unknown action without instrument", order{action: action(99), orderType: marketOrder}, true},
		{"unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: close, orderType: marketOrder}, true},

		// open needs a real direction
		{"open no direction", order{instrument: "EUR/USD", action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}, true},
		{"open unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}, true},

		// the strategy must set the stop on every open
		{"open long without a stop", order{instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder}, true},
		{"open short without a stop", order{instrument: "EUR/USD", direction: short, action: open, orderType: marketOrder}, true},
		{"open with a negative stop", order{instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, stopLoss: -1}, true},

		// add/reduce/close act on the existing position and take no direction
		{"close with long", order{instrument: "EUR/USD", direction: long, action: close, orderType: marketOrder}, true},
		{"close with short", order{instrument: "EUR/USD", direction: short, action: close, orderType: marketOrder}, true},
		{"add with direction", order{instrument: "EUR/USD", direction: long, action: add, orderType: marketOrder}, true},
		{"reduce with direction", order{instrument: "EUR/USD", direction: short, action: reduce, orderType: marketOrder}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.Time = testStart
			err := tt.in.validate()
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidOrder)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestOrder_Validate_NeedsTheBarTime(t *testing.T) {
	err := (&order{instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}).validate()

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "time")
}

func TestOrder_Validate_StopErrorNamesTheProblem(t *testing.T) {
	err := (&order{Time: testStart, instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder}).validate()
	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "stop loss")
}

func TestOrder_Validate_CloseNeedsNoStop(t *testing.T) {
	require.NoError(t, (&order{Time: testStart, instrument: "EUR/USD", action: close, orderType: marketOrder}).validate())
}

func TestOrder_RecordSetsStateAppendsTimestampedHistoryAndLogs(t *testing.T) {
	logs := captureLogs(t)
	t0 := time.Date(2010, 1, 4, 9, 30, 0, 0, time.UTC)
	o := &order{id: "sim-1", instrument: "EUR/USD", direction: long, action: open, Time: t0}

	o.record(stateRequested, o.Time, "")
	o.Time = t0.Add(time.Second)
	o.record(stateRejected, o.Time, "insufficient margin")

	assert.Equal(t, stateRejected, o.state)
	assert.Equal(t, []orderEvent{
		{state: stateRequested, time: t0},
		{state: stateRejected, time: t0.Add(time.Second), reason: "insufficient margin"},
	}, o.history)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 2, "one log line per state change")
	for _, l := range lines {
		assert.Contains(t, l, "level=INFO")
		assert.Contains(t, l, "order state")
		assert.Contains(t, l, "order=sim-1")
		assert.Contains(t, l, "instrument=EUR/USD")
		assert.Contains(t, l, "action=open")
	}
	assert.Contains(t, lines[0], "to=requested")
	assert.NotContains(t, lines[0], "from=", "the first event has no previous state")
	assert.NotContains(t, lines[0], "reason=")
	assert.Contains(t, lines[1], "from=requested")
	assert.Contains(t, lines[1], "to=rejected")
	assert.Contains(t, lines[1], `reason="insufficient margin"`)
}

func TestOrder_RecordStampsHistoryAndMovesTheOrderTime(t *testing.T) {
	captureLogs(t)
	first := time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)

	o := &order{}
	o.record(stateSized, first, "")
	assert.Equal(t, first, o.Time)
	o.record(stateSubmitted, second, "")

	assert.Equal(t, first, o.history[0].time)
	assert.Equal(t, second, o.history[1].time, "a later step carries the later bar's time")
	assert.Equal(t, second, o.Time, "the order is on its latest step")
}

func TestOrderReason_String(t *testing.T) {
	assert.Equal(t, "strategy", reasonStrategy.String())
	assert.Equal(t, "stop loss", reasonStopLoss.String())
	assert.Equal(t, "margin closeout", reasonMarginCloseout.String())
	assert.Equal(t, "unknown", orderReason(99).String())
}

func TestOrder_ReasonDefaultsToTheStrategy(t *testing.T) {
	assert.Equal(t, reasonStrategy, (&order{}).reason)
}

func TestOrder_RecordLogsTheCauseOnlyWhenItIsNotTheStrategy(t *testing.T) {
	logs := captureLogs(t)

	(&order{id: "1", action: open}).record(stateRequested, time.Time{}, "")
	assert.NotContains(t, logs.String(), "cause=")

	(&order{id: "2", action: close, reason: reasonMarginCloseout}).record(stateRequested, time.Time{}, "")
	assert.Contains(t, logs.String(), `cause="margin closeout"`)
}

func TestOrder_Validate_OpenNeedsAStopFromTheStrategy(t *testing.T) {
	for _, d := range []direction{long, short} {
		o := openOrderAt("EURUSD", d, eurusd1_10)
		o.stopLoss = 0 // the strategy did not set one

		err := o.validate()

		require.ErrorIs(t, err, errInvalidOrder, d.String())
		assert.Contains(t, err.Error(), "requires a stop loss")
		assert.False(t, isRejection(err), "a missing stop is a strategy bug, not a trading outcome")
		assert.Zero(t, o.stopLoss, "the account never invents a stop")
	}
}

func TestOrder_Validate_StopMustBeOnTheLosingSide(t *testing.T) {
	for _, tt := range []struct {
		d    direction
		stop price
	}{
		{long, 111000},  // above a long entry
		{long, 110000},  // at the entry
		{short, 109000}, // below a short entry
		{short, 110000},
	} {
		o := openOrderAt("EURUSD", tt.d, eurusd1_10)
		o.stopLoss = tt.stop

		err := o.validate()

		require.ErrorIs(t, err, errInvalidOrder, "%s stop %d", tt.d, tt.stop)
		assert.False(t, isRejection(err), "a bad stop is a strategy bug, not a trading outcome")
	}
}

func TestOrder_Validate_OpenNeedsAReferencePrice(t *testing.T) {
	o := openOrderAt("EURUSD", long, 0)
	o.stopLoss = 1

	err := o.validate()

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "requires a reference price")
}

func TestOrder_Validate_RefusesPricesAboveTheBound(t *testing.T) {
	over := func(set func(*order)) *order {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		set(o)
		return o
	}
	for name, o := range map[string]*order{
		"price":       over(func(o *order) { o.price = maxPrice + 1 }),
		"stop loss":   over(func(o *order) { o.stopLoss = maxPrice + 1 }),
		"take profit": over(func(o *order) { o.takeProfit = maxPrice + 1 }),
	} {
		assert.ErrorIs(t, o.validate(), errInvalidOrder, name)
	}

	atTheBound := openOrderAt("EURUSD", long, maxPrice)
	assert.NoError(t, atTheBound.validate(), "the bound itself is allowed")
}
