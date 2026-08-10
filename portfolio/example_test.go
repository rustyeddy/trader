package portfolio_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/portfolio"
)

// Example_newPortfolio aggregates a USD account and a EUR account into
// one USD-denominated Portfolio.
func Example_newPortfolio() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))
	usdAccountID, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}
	eurAccountID, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}

	usd := num.MustParseCurrency("USD")
	eur := num.MustParseCurrency("EUR")
	asOf := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	usdAccount, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       usdAccountID,
		Broker:          "OANDA",
		Currency:        usd,
		AsOf:            asOf,
		CashBalances:    []num.Money{num.MustParseMoney("10000", usd)},
		Equity:          num.MustParseMoney("10000", usd),
		BuyingPower:     num.MustParseMoney("10000", usd),
		MarginUsed:      num.MustParseMoney("0", usd),
		MarginAvailable: num.MustParseMoney("10000", usd),
		RealizedPnL:     num.MustParseMoney("0", usd),
		UnrealizedPnL:   num.MustParseMoney("0", usd),
		Fees:            num.MustParseMoney("0", usd),
		Financing:       num.MustParseMoney("0", usd),
	})
	if err != nil {
		panic(err)
	}

	eurAccount, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       eurAccountID,
		Broker:          "OANDA",
		Currency:        eur,
		AsOf:            asOf,
		CashBalances:    []num.Money{num.MustParseMoney("1000", eur)},
		Equity:          num.MustParseMoney("1000", eur),
		BuyingPower:     num.MustParseMoney("1000", eur),
		MarginUsed:      num.MustParseMoney("0", eur),
		MarginAvailable: num.MustParseMoney("1000", eur),
		RealizedPnL:     num.MustParseMoney("0", eur),
		UnrealizedPnL:   num.MustParseMoney("0", eur),
		Fees:            num.MustParseMoney("0", eur),
		Financing:       num.MustParseMoney("0", eur),
	})
	if err != nil {
		panic(err)
	}

	p, err := portfolio.NewPortfolio(portfolio.PortfolioParams{
		BaseCurrency: usd,
		AsOf:         asOf,
		Accounts:     []account.Snapshot{usdAccount, eurAccount},
		Rates: []portfolio.ConversionRate{
			{From: eur, To: usd, Rate: num.MustParseRate("1.10"), AsOf: asOf, Source: "ecb.reference"},
		},
	})
	if err != nil {
		panic(err)
	}

	equity, ok := p.Equity()
	fmt.Println(p.ConversionStatus(), ok, equity)
	// Output: complete true 11100 USD
}
