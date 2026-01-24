package market

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/pkg/broker"
)

type fakePriceSource struct {
	price          broker.Price
	err            error
	called         int
	lastInstrument string
}

func (f *fakePriceSource) GetPrice(ctx context.Context, instrument string) (broker.Price, error) {
	f.called++
	f.lastInstrument = instrument
	return f.price, f.err
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
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
	if err == nil {
		t.Fatalf("expected error for unknown instrument")
	}
	if rate != 0 {
		t.Fatalf("expected rate 0, got %v", rate)
	}
}

func TestQuoteToAccountRate_QuoteEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByQuote("USD")
	if !ok {
		t.Skip("no instrument with quote currency USD")
	}

	ps := &fakePriceSource{}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 1.0 {
		t.Fatalf("expected rate 1.0, got %v", rate)
	}
	if ps.called != 0 {
		t.Fatalf("expected no price lookup, got %d", ps.called)
	}
}

func TestQuoteToAccountRate_BaseEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByBase("USD")
	if !ok {
		t.Skip("no instrument with base currency USD")
	}

	ps := &fakePriceSource{
		price: broker.Price{Bid: 2.0, Ask: 4.0}, // mid = 3.0
	}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 1.0 / ps.price.Mid()
	if absFloat(rate-expected) > 1e-9 {
		t.Fatalf("expected rate %v, got %v", expected, rate)
	}
	if ps.called != 1 {
		t.Fatalf("expected 1 price lookup, got %d", ps.called)
	}
	if ps.lastInstrument != instrument {
		t.Fatalf("expected instrument %s, got %s", instrument, ps.lastInstrument)
	}
}

func TestQuoteToAccountRate_CrossNotImplemented(t *testing.T) {
	t.Parallel()

	instrument, ok := findCross("USD")
	if !ok {
		t.Skip("no cross instrument found")
	}

	ps := &fakePriceSource{}
	_, err := QuoteToAccountRate(instrument, "USD", ps)
	if err == nil {
		t.Fatalf("expected error for cross conversion")
	}
}
