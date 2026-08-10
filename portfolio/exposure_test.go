package portfolio

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortfolioExposuresGroupsAcrossAccountsByInstrument(t *testing.T) {
	accountA := mustAccountID(t)
	accountB := mustAccountID(t)

	// Same economic instrument (EUR/USD), reached through two different
	// venues/providers — exactly the cross-broker case portfolio exists
	// for.
	listingA := mustListing(t, "EUR", "USD", "OANDA", "EUR_USD")
	listingB := mustListing(t, "EUR", "USD", "IBKR", "EURUSD")

	posA := mustPosition(t, accountA, listingA)
	posB := mustPosition(t, accountB, listingB)

	paramsA := snapshotParamsFor(t, accountA, "USD", "10000")
	paramsA.Positions = []order.Position{posA}
	snapA, err := account.NewSnapshot(paramsA)
	require.NoError(t, err)

	paramsB := snapshotParamsFor(t, accountB, "USD", "5000")
	paramsB.Positions = []order.Position{posB}
	snapB, err := account.NewSnapshot(paramsB)
	require.NoError(t, err)

	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{snapA, snapB},
	})
	require.NoError(t, err)

	exposures := p.Exposures()
	require.Len(t, exposures, 1)
	assert.Equal(t, listingA.InstrumentID(), exposures[0].InstrumentID())

	contributors := exposures[0].Contributors()
	require.Len(t, contributors, 2)
	assert.Equal(t, accountA, contributors[0].AccountID)
	assert.Equal(t, accountB, contributors[1].AccountID)
}

func TestPortfolioExposuresSeparatesDifferentInstruments(t *testing.T) {
	acctID := mustAccountID(t)
	eurUsd := mustListing(t, "EUR", "USD", "OANDA", "EUR_USD")
	gbpUsd := mustListing(t, "GBP", "USD", "OANDA", "GBP_USD")

	posEur := mustPosition(t, acctID, eurUsd)
	posGbp := mustPosition(t, acctID, gbpUsd)

	params := snapshotParamsFor(t, acctID, "USD", "10000")
	params.Positions = []order.Position{posEur, posGbp}
	snap, err := account.NewSnapshot(params)
	require.NoError(t, err)

	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{snap},
	})
	require.NoError(t, err)

	exposures := p.Exposures()
	require.Len(t, exposures, 2)
}

func TestPortfolioExposuresContributorsIsDeepDefensiveCopy(t *testing.T) {
	acctID := mustAccountID(t)
	listing := mustListing(t, "EUR", "USD", "OANDA", "EUR_USD")
	pos := mustPosition(t, acctID, listing)

	params := snapshotParamsFor(t, acctID, "USD", "10000")
	params.Positions = []order.Position{pos}
	snap, err := account.NewSnapshot(params)
	require.NoError(t, err)

	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{snap},
	})
	require.NoError(t, err)

	got := p.Exposures()
	require.Len(t, got, 1)
	contributors := got[0].Contributors()
	require.Len(t, contributors, 1)
	original := *contributors[0].Position.AvgPrice

	*contributors[0].Position.AvgPrice = num.MustParsePrice("999")
	contributors[0].Position.Side = order.Short

	again := p.Exposures()[0].Contributors()
	require.Len(t, again, 1)
	assert.Equal(t, order.Long, again[0].Position.Side)
	assert.Equal(t, original, *again[0].Position.AvgPrice)
}

// snapshotParamsFor is like snapshotParams but with a caller-supplied
// AccountID, so multiple related snapshots can be built for the same
// portfolio test.
func snapshotParamsFor(t *testing.T, accountID id.AccountID, currency, equity string) account.SnapshotParams {
	t.Helper()
	p := snapshotParams(t, currency, equity)
	p.AccountID = accountID
	return p
}
