package instrument_test

import (
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

// Example_listing shows a Listing referencing an Instrument by ID, with
// its own venue-specific symbol and trading mechanics.
func Example_listing() {
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

	listing, err := instrument.NewListing(instrument.ListingParams{
		InstrumentID: eurUsd.ID(),
		Venue:        "OANDA",
		Symbol:       "EUR_USD",
		Spec:         spec,
		Tradable:     true,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(listing.Venue())
	fmt.Println(listing.Symbol())
	fmt.Println(listing.Tradable())
	fmt.Println(listing.InstrumentID().Equal(eurUsd.ID()))
	// Output:
	// OANDA
	// EUR_USD
	// true
	// true
}
