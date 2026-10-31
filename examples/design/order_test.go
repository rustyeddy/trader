package main

import (
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
		{"open long", order{instrument: "EUR/USD", direction: long, action: open}, false},
		{"open short", order{instrument: "EUR/USD", direction: short, action: open}, false},
		{"close without direction", order{instrument: "EUR/USD", action: close}, false},

		// no action
		{"zero value", order{}, true},
		{"instrument only", order{instrument: "EUR/USD"}, true},

		// missing instrument on every action
		{"open missing instrument", order{direction: long, action: open}, true},
		{"add missing instrument", order{action: add}, true},
		{"reduce missing instrument", order{action: reduce}, true},
		{"close missing instrument", order{action: close}, true},

		// invalid encodings
		{"unknown action", order{instrument: "EUR/USD", action: action(99)}, true},
		{"unknown action without instrument", order{action: action(99)}, true},
		{"unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: close}, true},

		// open needs a real direction
		{"open no direction", order{instrument: "EUR/USD", action: open}, true},
		{"open unknown direction", order{instrument: "EUR/USD", direction: direction(99), action: open}, true},

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

func TestOrder_RecordSetsStateAndAppendsHistory(t *testing.T) {
	o := &order{instrument: "EUR/USD", direction: long, action: open}
	t0 := time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)
	t1 := t0.AddDate(0, 0, 1)

	o.record(stateRequested, t0, "")
	o.record(stateRejected, t1, "insufficient margin")

	assert.Equal(t, stateRejected, o.state)
	assert.Equal(t, []orderEvent{
		{state: stateRequested, time: t0},
		{state: stateRejected, time: t1, reason: "insufficient margin"},
	}, o.history)
}
