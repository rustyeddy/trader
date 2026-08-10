package portfolio

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPortfolioSingleAccountSameCurrency(t *testing.T) {
	acct := mustSnapshot(t, "USD", "10000")
	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{acct},
	})
	require.NoError(t, err)

	assert.Equal(t, ConversionComplete, p.ConversionStatus())
	equity, ok := p.Equity()
	require.True(t, ok)
	assert.Equal(t, "10000 USD", equity.String())
	assert.Empty(t, p.MissingCurrencies())
	assert.Empty(t, p.ConversionRates())
}

func TestNewPortfolioMultiAccountSameCurrency(t *testing.T) {
	a := mustSnapshot(t, "USD", "10000")
	b := mustSnapshot(t, "USD", "5000")
	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{a, b},
	})
	require.NoError(t, err)

	equity, ok := p.Equity()
	require.True(t, ok)
	assert.Equal(t, "15000 USD", equity.String())
}

func TestNewPortfolioMultiCurrencyComplete(t *testing.T) {
	usdAcct := mustSnapshot(t, "USD", "10000")
	eurAcct := mustSnapshot(t, "EUR", "1000")

	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{usdAcct, eurAcct},
		Rates: []ConversionRate{
			{
				From:   num.MustParseCurrency("EUR"),
				To:     num.MustParseCurrency("USD"),
				Rate:   num.MustParseRate("1.10"),
				AsOf:   time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
				Source: "ecb.reference",
			},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, ConversionComplete, p.ConversionStatus())
	equity, ok := p.Equity()
	require.True(t, ok)
	assert.Equal(t, "11100 USD", equity.String())

	rates := p.ConversionRates()
	require.Len(t, rates, 1)
	assert.Equal(t, "ecb.reference", rates[0].Source)
}

func TestNewPortfolioMissingRateIsIncomplete(t *testing.T) {
	usdAcct := mustSnapshot(t, "USD", "10000")
	eurAcct := mustSnapshot(t, "EUR", "1000")
	gbpAcct := mustSnapshot(t, "GBP", "500")

	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{usdAcct, eurAcct, gbpAcct},
		Rates: []ConversionRate{
			{
				From: num.MustParseCurrency("EUR"),
				To:   num.MustParseCurrency("USD"),
				Rate: num.MustParseRate("1.10"),
				AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
			},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, ConversionIncomplete, p.ConversionStatus())
	_, ok := p.Equity()
	assert.False(t, ok)

	missing := p.MissingCurrencies()
	require.Len(t, missing, 1)
	assert.Equal(t, "GBP", missing[0].String())

	// ConversionRates must not report a partial, seemingly-usable subset
	// when Equity itself could not be computed.
	assert.Empty(t, p.ConversionRates())
}

func TestNewPortfolioRejectsInvalidBaseCurrency(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		AsOf:     time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts: []account.Snapshot{mustSnapshot(t, "USD", "1")},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsZeroAsOf(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		Accounts:     []account.Snapshot{mustSnapshot(t, "USD", "1")},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsNoAccounts(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsDuplicateAccountID(t *testing.T) {
	acct := mustSnapshot(t, "USD", "1")
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{acct, acct},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsRateNotTargetingBase(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{mustSnapshot(t, "EUR", "1")},
		Rates: []ConversionRate{
			{
				From: num.MustParseCurrency("EUR"),
				To:   num.MustParseCurrency("GBP"),
				Rate: num.MustParseRate("1"),
				AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
			},
		},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsNonPositiveRate(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{mustSnapshot(t, "EUR", "1")},
		Rates: []ConversionRate{
			{
				From: num.MustParseCurrency("EUR"),
				To:   num.MustParseCurrency("USD"),
				Rate: num.MustParseRate("0"),
				AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
			},
		},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsDuplicateRateCurrency(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{mustSnapshot(t, "EUR", "1")},
		Rates: []ConversionRate{
			{From: num.MustParseCurrency("EUR"), To: num.MustParseCurrency("USD"), Rate: num.MustParseRate("1.10"), AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC)},
			{From: num.MustParseCurrency("EUR"), To: num.MustParseCurrency("USD"), Rate: num.MustParseRate("1.11"), AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC)},
		},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestNewPortfolioRejectsRateFromEqualsBase(t *testing.T) {
	_, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{mustSnapshot(t, "USD", "1")},
		Rates: []ConversionRate{
			{From: num.MustParseCurrency("USD"), To: num.MustParseCurrency("USD"), Rate: num.MustParseRate("1"), AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC)},
		},
	})
	require.ErrorIs(t, err, ErrInvalidPortfolio)
}

func TestPortfolioAccountsPreservesProvenance(t *testing.T) {
	a := mustSnapshot(t, "USD", "10000")
	b := mustSnapshot(t, "EUR", "1000")
	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{a, b},
		Rates: []ConversionRate{
			{From: num.MustParseCurrency("EUR"), To: num.MustParseCurrency("USD"), Rate: num.MustParseRate("1.1"), AsOf: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC)},
		},
	})
	require.NoError(t, err)

	got := p.Accounts()
	require.Len(t, got, 2)
	assert.Equal(t, a.AccountID(), got[0].AccountID())
	assert.Equal(t, b.AccountID(), got[1].AccountID())
}

func TestPortfolioAccountAsOfRange(t *testing.T) {
	a := mustSnapshot(t, "USD", "10000")
	p, err := NewPortfolio(PortfolioParams{
		BaseCurrency: num.MustParseCurrency("USD"),
		AsOf:         time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC),
		Accounts:     []account.Snapshot{a},
	})
	require.NoError(t, err)

	oldest, newest := p.AccountAsOfRange()
	assert.Equal(t, a.AsOf(), oldest)
	assert.Equal(t, a.AsOf(), newest)
}
