package marketdata_test

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Example_interval shows the predefined intervals and their display form.
// String is for display only — never parsed back into an Interval.
func Example_interval() {
	fmt.Println(marketdata.M1, marketdata.H1, marketdata.H4, marketdata.D1, marketdata.W1)
	// Output:
	// M1 H1 H4 D1 W1
}

// ExampleFXCalendar_Bar shows that a daily bar aligns to FX's 17:00 New
// York rollover, not to UTC midnight.
func ExampleFXCalendar_Bar() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	c := marketdata.NewFXCalendar(marketdata.FXCalendarParams{})

	wednesdayMorning := time.Date(2026, time.January, 7, 9, 0, 0, 0, loc)
	bar, err := c.Bar(wednesdayMorning, marketdata.D1)
	if err != nil {
		panic(err)
	}

	fmt.Println(bar.Start().In(loc))
	fmt.Println(bar.End().In(loc))
	// Output:
	// 2026-01-06 17:00:00 -0500 EST
	// 2026-01-07 17:00:00 -0500 EST
}

// ExampleBarSet shows a homogeneous set of bid-basis bars and how the
// half-open range and mid close are derived rather than stored.
func ExampleBarSet() {
	open := time.Date(2020, time.March, 2, 0, 0, 0, 0, time.UTC)
	bar := marketdata.Bar{
		Time:      open,
		Open:      num.MustParsePrice("1.10000"),
		High:      num.MustParsePrice("1.10250"),
		Low:       num.MustParsePrice("1.09900"),
		Close:     num.MustParsePrice("1.10100"),
		AvgSpread: num.MustParsePrice("0.00012"),
		MaxSpread: num.MustParsePrice("0.00030"),
		Ticks:     4213,
	}

	span, err := marketdata.NewTimeRange(open, open.Add(time.Hour))
	if err != nil {
		panic(err)
	}
	set := marketdata.BarSet{
		Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		Interval:   marketdata.H1,
		Span:       span,
		Basis:      marketdata.BasisBid,
		Bars:       []marketdata.Bar{bar},
	}
	if err := set.Validate(); err != nil {
		panic(err)
	}

	mid, err := bar.Mid()
	if err != nil {
		panic(err)
	}
	fmt.Println(set.Basis, set.Len(), mid)
	// Output:
	// bid 1 1.10106
}

// ExampleFXCalendar_Session shows that Session reports ok=false outside
// the FX trading week.
func ExampleFXCalendar_Session() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	c := marketdata.NewFXCalendar(marketdata.FXCalendarParams{})

	saturday := time.Date(2026, time.January, 3, 12, 0, 0, 0, loc)
	fmt.Println(c.Status(saturday))
	_, ok := c.Session(saturday)
	fmt.Println(ok)
	// Output:
	// closed
	// false
}

// ExampleManager_Bars shows the only way to reach historical canonical
// data: constructing a Manager and calling Bars. testdata/ holds a
// checked-in canonical EUR/USD H1 partition; Manager has no exported
// build/publish operation yet (that lands in a later M2 issue), so this
// example reads a partition placed there ahead of time rather than
// publishing one itself.
func ExampleManager_Bars() {
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
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
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		panic(err)
	}
	resolver := instrument.NewMemoryResolver()
	if err := resolver.Register(listing); err != nil {
		panic(err)
	}

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2020, time.March, 3, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    "testdata",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	if err != nil {
		panic(err)
	}

	start := time.Date(2020, time.March, 2, 0, 0, 0, 0, time.UTC)
	span, err := marketdata.NewTimeRange(start, start.Add(4*time.Hour))
	if err != nil {
		panic(err)
	}

	reader, err := mgr.Bars(context.Background(), marketdata.BarQuery{
		Instrument: eurusd.ID(),
		Interval:   marketdata.H1,
		Range:      span,
	})
	if err != nil {
		panic(err)
	}
	defer reader.Close()

	for {
		bar, err := reader.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		fmt.Println(bar.Time.Format(time.RFC3339), bar.Close)
	}
	// Output:
	// 2020-03-02T00:00:00Z 1.101
	// 2020-03-02T01:00:00Z 1.101
}
