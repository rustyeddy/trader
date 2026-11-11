package instrument_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Example_currencyPair shows that EUR/USD resolves to the same canonical
// identity regardless of which provider spelling produced the base and
// quote currencies passed in.
func Example_currencyPair() {
	eur := num.MustParseCurrency("EUR")
	usd := num.MustParseCurrency("USD")

	fromOanda, err := instrument.NewCurrencyPair(eur, usd) // parsed from "EUR_USD"
	if err != nil {
		panic(err)
	}
	fromAnotherProvider, err := instrument.NewCurrencyPair(eur, usd) // parsed from "EURUSD"
	if err != nil {
		panic(err)
	}

	fmt.Println(fromOanda.ID())
	fmt.Println(fromOanda.ID().Equal(fromAnotherProvider.ID()))
	// Output:
	// fx:EUR/USD
	// true
}

// Example_equity shows a simple equity instrument and its identity.
func Example_equity() {
	apple, err := instrument.NewEquity("NASDAQ", "AAPL")
	if err != nil {
		panic(err)
	}

	fmt.Println(apple.Kind())
	fmt.Println(apple.ID())
	// Output:
	// equity
	// eq:NASDAQ:AAPL
}

// Example_future shows that two futures contracts on the same underlying
// root but different expirations are distinct Instruments, and that a
// continuous research series on the same root is distinct from either.
func Example_future() {
	dec, err := instrument.NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		panic(err)
	}
	mar, err := instrument.NewFuture("ES", time.Date(2027, time.March, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		panic(err)
	}
	continuous, err := instrument.NewContinuousSeries("ES")
	if err != nil {
		panic(err)
	}

	fmt.Println(dec.ID())
	fmt.Println(mar.ID())
	fmt.Println(continuous.ID())
	fmt.Println(dec.ID().Equal(mar.ID()))
	fmt.Println(dec.ID().Equal(continuous.ID()))
	// Output:
	// fut:ES:2026-12
	// fut:ES:2027-03
	// cont:ES
	// false
	// false
}

// Example_listing shows a Listing referencing an Instrument, with its own
// provider, symbol, and trading mechanics. Provider and Venue are kept
// distinct: IBKR (the provider) exposes a NASDAQ (the venue) listing of
// Apple.
func Example_listing() {
	apple, err := instrument.NewEquity("NASDAQ", "AAPL")
	if err != nil {
		panic(err)
	}

	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	if err != nil {
		panic(err)
	}

	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: apple,
		Provider:   "IBKR",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(listing.Provider())
	fmt.Println(listing.Venue())
	fmt.Println(listing.Symbol())
	fmt.Println(listing.Tradable())
	fmt.Println(listing.InstrumentID().Equal(apple.ID()))
	// Output:
	// IBKR
	// NASDAQ
	// AAPL
	// true
	// true
}

// Example_resolver shows registering EUR/USD under two different
// providers' symbol spellings, plus an alias, and resolving all three back
// to the same Instrument without any of them becoming its identity.
func Example_resolver() {
	eurUsd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	if err != nil {
		panic(err)
	}

	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	if err != nil {
		panic(err)
	}

	r := instrument.NewMemoryResolver()

	oanda, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurUsd,
		Provider:   "OANDA",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		panic(err)
	}
	if err := r.Register(oanda); err != nil {
		panic(err)
	}

	// A convenience spelling application code might use, registered as an
	// alias rather than a second Listing.
	if err := r.RegisterAlias("OANDA", "", "EURUSD", "OANDA", "", "EUR_USD"); err != nil {
		panic(err)
	}

	bySymbol, err := r.ResolveSymbol("OANDA", "", "EUR_USD")
	if err != nil {
		panic(err)
	}
	byAlias, err := r.ResolveSymbol("OANDA", "", "EURUSD")
	if err != nil {
		panic(err)
	}
	byInstrument, err := r.ResolveInstrument(eurUsd.ID(), "OANDA", "")
	if err != nil {
		panic(err)
	}

	fmt.Println(bySymbol.Symbol())
	fmt.Println(byAlias.Symbol()) // the alias resolves to the canonical Listing's own symbol, not "EURUSD"
	fmt.Println(byInstrument.Symbol())
	// Output:
	// EUR_USD
	// EUR_USD
	// EUR_USD
}

// Example_resolverAmbiguous shows an unconstrained lookup failing because
// one provider exposes the same symbol on two venues, and resolved by
// supplying the venue.
func Example_resolverAmbiguous() {
	apple, err := instrument.NewEquity("NASDAQ", "AAPL")
	if err != nil {
		panic(err)
	}

	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	if err != nil {
		panic(err)
	}

	r := instrument.NewMemoryResolver()
	for _, venue := range []string{"NASDAQ", "BATS"} {
		l, err := instrument.NewListing(instrument.ListingParams{
			Instrument: apple,
			Provider:   "IBKR",
			Venue:      venue,
			Symbol:     "AAPL",
			Spec:       spec,
			Tradable:   true,
		})
		if err != nil {
			panic(err)
		}
		if err := r.Register(l); err != nil {
			panic(err)
		}
	}

	_, err = r.ResolveSymbol("IBKR", "", "AAPL")
	fmt.Println(errors.Is(err, instrument.ErrAmbiguousSymbol))

	got, err := r.ResolveSymbol("IBKR", "BATS", "AAPL")
	if err != nil {
		panic(err)
	}
	fmt.Println(got.Venue())
	// Output:
	// true
	// BATS
}
