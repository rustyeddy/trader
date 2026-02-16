package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPriceStore_SetGet(t *testing.T) {
	t.Parallel()

	ps := NewTickStore()
	p := Tick{
		Instrument: "EUR_USD",
		Bid:        1.1,
		Ask:        1.2,
	}

	ps.Set(p)

	got, err := ps.Get("EUR_USD")
	assert.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestPriceStore_GetMissing(t *testing.T) {
	t.Parallel()

	ps := NewTickStore()

	got, err := ps.Get("NO_SUCH")
	assert.Error(t, err)
	assert.Equal(t, Tick{}, got)
}
