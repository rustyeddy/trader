package tradertest_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
)

// Example_buildOrderLifecycle is the acceptance scenario for issue #31:
// an external-package test builds an instrument, an order, a fill, and
// an account snapshot with little boilerplate, using only tradertest
// and the public M1 packages it builds on.
func Example_buildOrderLifecycle() {
	// Deterministic time and identifiers: tradertest does not wrap
	// these, so build them the same way any Trader consumer would.
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))
	accountID := tradertest.MustAccountID(g)

	listing := tradertest.MustNewListing(tradertest.ListingParams{})

	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:   listing,
		AccountID: accountID,
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	if err != nil {
		panic(err)
	}
	workingOrder := tradertest.MustNewOrder(tradertest.OrderParams{Request: request})

	fill := tradertest.MustNewFillFor(tradertest.FillParams{
		Order:  workingOrder,
		FillID: tradertest.MustFillID(g),
	})
	filledOrder, err := order.ApplyFill(workingOrder, fill)
	if err != nil {
		panic(err)
	}

	position := tradertest.MustNewPosition(tradertest.PositionParams{
		AccountID: accountID,
		Listing:   listing,
	})

	snapshot := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID:  accountID,
		Positions:  []order.Position{position},
		OpenOrders: []order.Order{},
	})

	fmt.Println(filledOrder.Status, len(snapshot.Positions()), snapshot.Broker())
	// Output: filled 1 OANDA
}

// Example_newSnapshot shows a minimal account.Snapshot built with only
// the fields a given test cares about — the rest default to zero or to
// the snapshot's own Equity, rather than requiring all nine num.Money
// fields to be populated by hand.
func Example_newSnapshot() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))

	snapshot := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
		Equity:    "25000",
	})

	fmt.Println(snapshot.Broker(), snapshot.Equity())
	// Output: OANDA 25000 USD
}

// Example_newPortfolio aggregates two account snapshots already in the
// portfolio's base currency, so no conversion rate is required.
func Example_newPortfolio() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))

	a := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
		Equity:    "10000",
	})
	b := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
		Broker:    "IBKR",
		Equity:    "5000",
	})

	p := tradertest.MustNewPortfolio(tradertest.PortfolioParams{
		Accounts: []account.Snapshot{a, b},
	})

	equity, _ := p.Equity()
	fmt.Println(p.ConversionStatus(), equity)
	// Output: complete 15000 USD
}
