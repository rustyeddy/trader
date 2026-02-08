package market

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
)

type fakePriceSource struct {
	price          pricing.Tick
	err            error
	called         int
	lastInstrument string
}

func (f *fakePriceSource) GetTick(ctx context.Context, instrument string) (pricing.Tick, error) {
	f.called++
	f.lastInstrument = instrument
	return f.price, f.err
}

func findByQuote(account string) (string, bool) {
	for k, v := range Instruments {
		if v.QuoteCurrency == account {
			return k, true
		}
	}
	return "", false
}

func findByBase(account string) (string, bool) {
	for k, v := range Instruments {
		if v.BaseCurrency == account {
			return k, true
		}
	}
	return "", false
}

func findCross(account string) (string, bool) {
	for k, v := range Instruments {
		if v.BaseCurrency != account && v.QuoteCurrency != account {
			return k, true
		}
	}
	return "", false
}

func TestQuoteToAccountRate_UnknownInstrument(t *testing.T) {
	t.Parallel()

	ps := &fakePriceSource{}
	rate, err := QuoteToAccountRate("NO_SUCH_INSTRUMENT", "USD", ps)
	assert.Error(t, err)
	assert.Equal(t, 0.0, rate)
}

func TestQuoteToAccountRate_QuoteEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByQuote("USD")
	if !ok {
		t.Skip("no instrument with quote currency USD")
	}

	ps := &fakePriceSource{}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.NoError(t, err)
	assert.Equal(t, 1.0, rate)
	assert.Equal(t, 0, ps.called)
}

func TestQuoteToAccountRate_BaseEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByBase("USD")
	if !ok {
		t.Skip("no instrument with base currency USD")
	}

	ps := &fakePriceSource{
		price: pricing.Tick{Bid: 2.0, Ask: 4.0}, // mid = 3.0
	}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.NoError(t, err)

	expected := 1.0 / ps.price.Mid()
	assert.InDelta(t, expected, rate, 1e-9)
	assert.Equal(t, 1, ps.called)
	assert.Equal(t, instrument, ps.lastInstrument)
}

func TestQuoteToAccountRate_CrossNotImplemented(t *testing.T) {
	t.Parallel()

	instrument, ok := findCross("USD")
	if !ok {
		t.Skip("no cross instrument found")
	}

	ps := &fakePriceSource{}
	_, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.Error(t, err)
}
