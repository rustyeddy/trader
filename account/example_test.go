package account_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
)

// Example_newSnapshot builds a minimal, valid account.Snapshot: an
// account holding only its home currency, with no open positions or
// orders.
func Example_newSnapshot() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}

	usd := num.MustParseCurrency("USD")
	snapshot, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          "OANDA",
		Currency:        usd,
		AsOf:            time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
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

	fmt.Println(snapshot.Broker(), snapshot.Equity())
	// Output: OANDA 10000 USD
}
