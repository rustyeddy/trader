package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPortfolioDefaults(t *testing.T) {
	g := testGenerator()
	snapshot := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
	})

	p, err := tradertest.NewPortfolio(tradertest.PortfolioParams{
		Accounts: []account.Snapshot{snapshot},
	})
	require.NoError(t, err)

	assert.Equal(t, "USD", p.BaseCurrency().String())
	assert.True(t, tradertest.DefaultAsOf.Equal(p.AsOf()))
	assert.Equal(t, portfolio.ConversionComplete, p.ConversionStatus())
}

func TestNewPortfolioMultiCurrencyNeedsRates(t *testing.T) {
	g := testGenerator()
	usdSnapshot := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
	})
	eurSnapshot := tradertest.MustNewSnapshot(tradertest.SnapshotParams{
		AccountID: tradertest.MustAccountID(g),
		Currency:  "EUR",
		Broker:    "OANDA",
	})

	p, err := tradertest.NewPortfolio(tradertest.PortfolioParams{
		Accounts: []account.Snapshot{usdSnapshot, eurSnapshot},
	})
	require.NoError(t, err)
	assert.Equal(t, portfolio.ConversionIncomplete, p.ConversionStatus())
}

func TestNewPortfolioRejectsNoAccounts(t *testing.T) {
	_, err := tradertest.NewPortfolio(tradertest.PortfolioParams{})
	require.Error(t, err)
}

func TestMustNewPortfolioPanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		tradertest.MustNewPortfolio(tradertest.PortfolioParams{})
	})
}
