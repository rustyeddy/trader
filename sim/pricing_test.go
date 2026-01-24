package sim

import (
    "testing"

    "github.com/rustyeddy/trader/broker"
    "github.com/stretchr/testify/assert"
)

func TestPriceStore_SetGet(t *testing.T) {
    t.Parallel()

    ps := NewPriceStore()
    p := broker.Price{
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

    ps := NewPriceStore()

    got, err := ps.Get("NO_SUCH")
    assert.Error(t, err)
    assert.Equal(t, broker.Price{}, got)
}