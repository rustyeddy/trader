package marketdata_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// A consumer outside the package builds a bar from a two-sided quote, lets the
// midpoint be derived once, and reads "the price" without choosing a basis.
func Example() {
	px := num.MustParsePrice
	bid := marketdata.OHLC{Open: px("1.10000"), High: px("1.10100"), Low: px("1.09900"), Close: px("1.10050")}
	ask := marketdata.OHLC{Open: px("1.10010"), High: px("1.10111"), Low: px("1.09911"), Close: px("1.10061")}
	mid, err := marketdata.MidOf(bid, ask)
	if err != nil {
		panic(err)
	}
	open := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := marketdata.Bar{Time: open, AvailableAt: open.Add(time.Hour), Volume: 100, Bid: bid, Ask: ask, Mid: mid}

	fmt.Println(bar.Validate())
	fmt.Println(bar.OHLC().Close, bar.Bid.Close, bar.Ask.Close)
	fmt.Println(bar.IsAvailable(open.Add(30*time.Minute)), bar.IsAvailable(open.Add(time.Hour)))
	// Output:
	// <nil>
	// 1.100555 1.1005 1.10061
	// false true
}

// A single-price source writes the price to the series it is; nothing else is
// invented.
func Example_singlePriceSource() {
	px := num.MustParsePrice
	open := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := marketdata.Bar{Time: open, AvailableAt: open.Add(24 * time.Hour),
		Trade: marketdata.OHLC{Open: px("187.10"), High: px("188.00"), Low: px("186.50"), Close: px("187.90")}}

	fmt.Println(bar.Validate(), bar.OHLC().Close, bar.Bid.IsZero(), bar.Ask.IsZero())
	// Output: <nil> 187.9 true true
}
