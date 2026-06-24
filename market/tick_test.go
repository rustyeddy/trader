package market

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPriceStore_SetGet verifies expected behavior for this component.
func TestPriceStore_SetGet(t *testing.T) {
	t.Parallel()

	ps := newTickStore()
	p := Tick{
		Instrument: "EUR_USD",
		BA: BA{
			Bid: 11,
			Ask: 12,
		},
	}

	require.NoError(t, ps.Set(p))

	got, err := ps.Get("EUR_USD")
	assert.NoError(t, err)
	assert.Equal(t, "EURUSD", got.Instrument)
	assert.Equal(t, Tick{Instrument: "EURUSD", BA: BA{Bid: 11, Ask: 12}}, got)
}

// TestPriceStore_GetMissing verifies expected behavior for this component.
func TestPriceStore_GetMissing(t *testing.T) {
	t.Parallel()

	ps := newTickStore()

	got, err := ps.Get("NO_SUCH")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrTickNotFound))
	assert.Equal(t, Tick{}, got)
}

func TestPriceStore_NormalizesInstrument(t *testing.T) {
	t.Parallel()

	ps := newTickStore()
	require.NoError(t, ps.Set(Tick{
		Instrument: " eur_usd ",
		BA: BA{
			Bid: 100,
			Ask: 102,
		},
	}))

	got, err := ps.Get("EUR/USD")
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", got.Instrument)
}

// TestTickSpread_Phase1 verifies expected behavior for this component.
func TestTickSpread_Phase1(t *testing.T) {
	t.Parallel()

	tk := Tick{BA: BA{Bid: 100, Ask: 106}}
	assert.Equal(t, Price(6), tk.Spread())
}

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
		BA:         BA{Bid: 100, Ask: 102},
	}.Validate())

	require.EqualError(t, Tick{
		Instrument: "",
		BA:         BA{Bid: 100, Ask: 102},
	}.Validate(), "tick instrument must not be empty")

	require.EqualError(t, Tick{
		Instrument: "EURUSD",
		BA:         BA{Bid: 102, Ask: 100},
	}.Validate(), "ask must be >= bid")
}
