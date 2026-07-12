package market

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTickMid_RoundsHalfUp_Phase1 verifies expected behavior for this component.
func TestTickMid_RoundsHalfUp_Phase1(t *testing.T) {
	t.Parallel()

	even := Tick{BA: BA{Bid: 100, Ask: 104}}
	assert.Equal(t, Price(102), even.Mid())

	odd := Tick{BA: BA{Bid: 100, Ask: 103}}
	assert.Equal(t, Price(102), odd.Mid())
}

func TestPriceMid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bid      Price
		ask      Price
		expected Price
	}{
		{"simple", 10, 30, 20},
		{"same", 25, 25, 25},
		{"zero", 0, 0, 0},
		{"negative", -20, 20, 0},
		{"fractional", 11, 13, 12},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Tick{BA: BA{Bid: tt.bid, Ask: tt.ask}}
			assert.Equal(t, tt.expected, p.Mid())
		})
	}
}

func TestTickValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, Tick{
		Instrument: "EUR_USD",
		BA:         BA{Bid: 11, Ask: 12},
	}.Validate())

	require.Error(t, Tick{
		Instrument: "",
		BA:         BA{Bid: 11, Ask: 12},
	}.Validate())

	require.Error(t, Tick{
		Instrument: "EUR_USD",
		BA:         BA{Bid: 0, Ask: 12},
	}.Validate())

	require.Error(t, Tick{
		Instrument: "EUR_USD",
		BA:         BA{Bid: 12, Ask: 11},
	}.Validate())
}
