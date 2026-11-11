package num_test

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// Example demonstrates num's exact value types: Price and Quantity are
// backed by a scaled integer (ADR-004), never a raw float64, so
// arithmetic on them never introduces floating-point error the way
// 1.1+0.0001 would in binary floating point.
func Example() {
	price := num.MustParsePrice("1.10000")
	quantity := num.MustParseQuantity("1000")

	marked, err := price.Add(num.MustParsePrice("0.00010"))
	if err != nil {
		panic(err)
	}

	fmt.Println(price, quantity, marked)
	// Output: 1.1 1000 1.1001
}

// Example_money shows Money's mandatory currency and its one sanctioned
// currency-conversion primitive, Convert. Money.MulRate deliberately
// preserves currency and is not conversion — see the Money doc comment.
func Example_money() {
	usd := num.MustParseCurrency("USD")
	eur := num.MustParseCurrency("EUR")

	balance := num.MustParseMoney("1000", usd)
	converted, err := balance.Convert(eur, num.MustParseRate("0.92"))
	if err != nil {
		panic(err)
	}

	fmt.Println(balance, converted)
	// Output: 1000 USD 920 EUR
}
