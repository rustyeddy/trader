package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSnapshotDefaults(t *testing.T) {
	g := testGenerator()
	s, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
	})
	require.NoError(t, err)

	assert.Equal(t, "OANDA", s.Broker())
	assert.Equal(t, "USD", s.Currency().String())
	assert.True(t, tradertest.DefaultAsOf().Equal(s.AsOf()))
	assert.Empty(t, s.Positions())
	assert.Empty(t, s.OpenOrders())
	tradertest.AssertMoneyEqual(t, s.Equity(), s.BuyingPower())
}

func TestNewSnapshotWithPositionsAndOrders(t *testing.T) {
	g := testGenerator()
	accountID := tradertest.MustAccountID(g)
	listing := tradertest.MustNewListing(tradertest.ListingParams{})

	position := tradertest.MustNewPosition(tradertest.PositionParams{
		AccountID: accountID,
		Listing:   listing,
	})
	proposal := tradertest.MustNewProposal(tradertest.ProposalParams{
		Listing:   listing,
		AccountID: accountID,
	})
	request, err := order.NewRequest(proposal, tradertest.MustOrderID(g))
	require.NoError(t, err)
	workingOrder := tradertest.MustNewOrder(tradertest.OrderParams{Request: request})

	s, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID:  accountID,
		Positions:  []order.Position{position},
		OpenOrders: []order.Order{workingOrder},
	})
	require.NoError(t, err)
	assert.Len(t, s.Positions(), 1)
	assert.Len(t, s.OpenOrders(), 1)
}

func TestNewSnapshotExplicitCashBalances(t *testing.T) {
	g := testGenerator()
	usd := "USD"
	s, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID:    tradertest.MustAccountID(g),
		Currency:     usd,
		CashBalances: []num.Money{num.MustParseMoney("1", num.MustParseCurrency(usd)), num.MustParseMoney("2", num.MustParseCurrency("EUR"))},
	})
	require.NoError(t, err)
	assert.Len(t, s.CashBalances(), 2)
}

func TestNewSnapshotExplicitEmptyCashBalancesIsPreserved(t *testing.T) {
	g := testGenerator()
	s, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID:    tradertest.MustAccountID(g),
		CashBalances: []num.Money{},
	})
	require.NoError(t, err)
	// A nil CashBalances would have defaulted to one entry holding
	// Equity; an explicitly empty, non-nil slice must not be defaulted.
	assert.Empty(t, s.CashBalances())
}

func TestNewSnapshotRejectsInvalidCurrency(t *testing.T) {
	g := testGenerator()
	_, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
		Currency:  "not-a-currency",
	})
	require.Error(t, err)
}

func TestMustNewSnapshotPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewSnapshot(tradertest.SnapshotParams{})
	})
}
