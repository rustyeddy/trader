package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	ps.Set(p)

	got, err := ps.Get("EUR_USD")
	assert.NoError(t, err)
	assert.Equal(t, p, got)
}

// TestPriceStore_GetMissing verifies expected behavior for this component.
func TestPriceStore_GetMissing(t *testing.T) {
	t.Parallel()

	ps := newTickStore()

	got, err := ps.Get("NO_SUCH")
	assert.Error(t, err)
	assert.Equal(t, Tick{}, got)
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
