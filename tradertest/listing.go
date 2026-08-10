package tradertest

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// ListingParams builds a currency-pair instrument.Listing. Base, Quote,
// and Provider default to "EUR", "USD", and "OANDA"; Symbol defaults to
// Base+"_"+Quote. Venue defaults to empty, matching spot FX's lack of a
// centralized venue (see the instrument.Listing doc comment).
//
// Tick size, quantity increment, contract multiplier, and tradability
// are not parameterized: every existing M1 test fixture used the same
// values (0.00001 tick, 1 quantity increment, 1 multiplier, tradable),
// so exposing them here would be speculative rather than
// evidence-driven. A consumer needing different mechanics should call
// instrument.NewListing directly.
type ListingParams struct {
	Base     string
	Quote    string
	Provider string
	Venue    string
	Symbol   string
}

// NewListing returns a valid currency-pair instrument.Listing built
// from p, filling in defaults for any zero-valued field.
func NewListing(p ListingParams) (instrument.Listing, error) {
	if p.Base == "" {
		p.Base = "EUR"
	}
	if p.Quote == "" {
		p.Quote = "USD"
	}
	if p.Provider == "" {
		p.Provider = "OANDA"
	}
	if p.Symbol == "" {
		p.Symbol = fmt.Sprintf("%s_%s", p.Base, p.Quote)
	}

	base, err := num.ParseCurrency(p.Base)
	if err != nil {
		return instrument.Listing{}, err
	}
	quote, err := num.ParseCurrency(p.Quote)
	if err != nil {
		return instrument.Listing{}, err
	}
	inst, err := instrument.NewCurrencyPair(base, quote)
	if err != nil {
		return instrument.Listing{}, err
	}

	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quote,
	)
	if err != nil {
		return instrument.Listing{}, err
	}

	return instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   p.Provider,
		Venue:      p.Venue,
		Symbol:     p.Symbol,
		Spec:       spec,
		Tradable:   true,
	})
}

// MustNewListing is like NewListing but panics on error.
func MustNewListing(p ListingParams) instrument.Listing {
	l, err := NewListing(p)
	if err != nil {
		panic(err)
	}
	return l
}
