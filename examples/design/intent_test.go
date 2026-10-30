package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAction_String(t *testing.T) {
	tests := map[action]string{
		hold:       "hold",
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

func TestIntent_EnumValuesAreStable(t *testing.T) {
	assert.Equal(t, direction(0), noDirection)
	assert.Equal(t, direction(1), long)
	assert.Equal(t, direction(2), short)
	assert.Equal(t, action(0), hold)
}

func TestIntent_ZeroValueIsHold(t *testing.T) {
	var i intent
	assert.Equal(t, hold, i.action)
	assert.Equal(t, noDirection, i.direction)
	assert.Empty(t, i.instrument)
	assert.NoError(t, i.validate())
}

func TestIntent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		in      intent
		wantErr bool
	}{
		// valid
		{"hold zero value", intent{}, false},
		{"hold with instrument", intent{instrument: "EUR/USD"}, false},
		{"open long", intent{instrument: "EUR/USD", direction: long, action: open}, false},
		{"open short", intent{instrument: "EUR/USD", direction: short, action: open}, false},
		{"close without direction", intent{instrument: "EUR/USD", action: close}, false},

		// missing instrument on actionable intents
		{"open missing instrument", intent{direction: long, action: open}, true},
		{"add missing instrument", intent{action: add}, true},
		{"reduce missing instrument", intent{action: reduce}, true},
		{"close missing instrument", intent{action: close}, true},

		// invalid encodings must not silently become hold
		{"unknown action", intent{instrument: "EUR/USD", action: action(99)}, true},
		{"unknown action without instrument", intent{action: action(99)}, true},
		{"unknown direction on hold", intent{direction: direction(99)}, true},

		// open needs a real direction
		{"open no direction", intent{instrument: "EUR/USD", action: open}, true},
		{"open unknown direction", intent{instrument: "EUR/USD", direction: direction(99), action: open}, true},

		// add/reduce/close act on the existing position and take no direction
		{"close with long", intent{instrument: "EUR/USD", direction: long, action: close}, true},
		{"close with short", intent{instrument: "EUR/USD", direction: short, action: close}, true},
		{"add with direction", intent{instrument: "EUR/USD", direction: long, action: add}, true},
		{"reduce with direction", intent{instrument: "EUR/USD", direction: short, action: reduce}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.validate()
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidIntent)
				return
			}
			require.NoError(t, err)
		})
	}
}
