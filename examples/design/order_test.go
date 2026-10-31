package main

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
	require.ErrorIs(t, (&order{}).validate(), errInvalidOrder)
	require.ErrorIs(t, (&order{instrument: "EUR/USD"}).validate(), errInvalidOrder)
}

func TestOrder_Validate(t *testing.T) {
	tests := []struct {
		name    string
		in      order
		wantErr bool
	}{
		// valid
		{"open long", order{instrument: "EUR/USD", direction: long, action: open, stopLoss: 108900}, false},
		{"open short", order{instrument: "EUR/USD", direction: short, action: open, stopLoss: 111100}, false},
		{"close without direction", order{instrument: "EUR/USD", action: close}, false},

		// no action
		{"zero value", order{}, true},
		{"instrument only", order{instrument: "EUR/USD"}, true},

		// missing instrument on every action
		{"open missing instrument", order{direction: long, action: open, stopLoss: 108900}, true},
		{"add missing instrument", order{action: add}, true},
		{"reduce missing instrument", order{action: reduce}, true},
		{"close missing instrument", order{action: close}, true},

		// invalid encodings
		{"unknown action", order{instrument: "EUR/USD", action: action(99)}, true},
		{"unknown action without instrument", order{action: action(99)}, true},
		{"unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: close}, true},

		// open needs a real direction
		{"open no direction", order{instrument: "EUR/USD", action: open, stopLoss: 108900}, true},
		{"open unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: open, stopLoss: 108900}, true},

		// the strategy must set the stop on every open
		{"open long without a stop", order{instrument: "EUR/USD", direction: long, action: open}, true},
		{"open short without a stop", order{instrument: "EUR/USD", direction: short, action: open}, true},
		{"open with a negative stop", order{instrument: "EUR/USD", direction: long, action: open, stopLoss: -1}, true},

		// add/reduce/close act on the existing position and take no direction
		{"close with long", order{instrument: "EUR/USD", direction: long, action: close}, true},
		{"close with short", order{instrument: "EUR/USD", direction: short, action: close}, true},
		{"add with direction", order{instrument: "EUR/USD", direction: long, action: add}, true},
		{"reduce with direction", order{instrument: "EUR/USD", direction: short, action: reduce}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.validate()
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidOrder)
				return
			}
			require.NoError(t, err)
		})
	}
}

// stubClock replaces the order time source with one that returns start, then
// one second later on each following call.
func stubClock(t *testing.T, start time.Time) {
	t.Helper()
	prev := now
	next := start
	now = func() time.Time {
		cur := next
		next = next.Add(time.Second)
		return cur
	}
	t.Cleanup(func() { now = prev })
}

func TestOrder_Validate_StopErrorNamesTheProblem(t *testing.T) {
	err := (&order{instrument: "EUR/USD", direction: long, action: open}).validate()
	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "stop loss")
}

func TestOrder_Validate_CloseNeedsNoStop(t *testing.T) {
	require.NoError(t, (&order{instrument: "EUR/USD", action: close}).validate())
}

func TestOrder_RecordSetsStateAppendsTimestampedHistoryAndLogs(t *testing.T) {
	logs := captureLogs(t)
	t0 := time.Date(2010, 1, 4, 9, 30, 0, 0, time.UTC)
	stubClock(t, t0)
	o := &order{id: "sim-1", instrument: "EUR/USD", direction: long, action: open}

	o.record(stateRequested, "")
	o.record(stateRejected, "insufficient margin")

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

func TestOrder_RecordUsesTheTimeSource(t *testing.T) {
	captureLogs(t)
	fixed := time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)
	prev := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = prev })

	o := &order{}
	o.record(stateSized, "")
	assert.Equal(t, fixed, o.history[0].time)
}
