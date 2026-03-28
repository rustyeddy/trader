package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePrices implements market.TickSource for testing.
type fakePrices struct {
	ticks map[string]market.Tick
	err   error
}

func (f *fakePrices) GetTick(_ context.Context, instrument string) (market.Tick, error) {
	if f.err != nil {
		return market.Tick{}, f.err
	}
	t, ok := f.ticks[instrument]
	if !ok {
		return market.Tick{}, errors.New("no tick for " + instrument)
	}
	return t, nil
}

func usdAccount() account.Account {
	return account.Account{
		ID:       "test",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(100_000),
		Equity:   types.MoneyFromFloat(100_000),
	}
}

// TestQuoteToRate_QuoteCurrencyIsAccountCurrency tests pairs like EURUSD for a
// USD-denominated account.  The quote currency is USD == account currency, so
// the conversion rate must be exactly 1.0 (== RateScale).
func TestQuoteToRate_QuoteCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	prices := &fakePrices{}

	rate, err := acct.QuoteToAccount(context.Background(), "EURUSD", prices)
	require.NoError(t, err)
	assert.Equal(t, types.Rate(types.RateScale), rate)
}

// TestQuoteAccount_BaseCurrencyIsAccountCurrency tests pairs like USDJPY for a
// USD-denominated account.  The base currency is USD == account currency, so
// the conversion rate is 1/mid.
func TestQuoteAccount_BaseCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	prices := &fakePrices{
		ticks: map[string]market.Tick{
			"USDJPY": {
				Instrument: "USDJPY",
				BA: market.BA{
					Bid: types.PriceFromFloat(150.00),
					Ask: types.PriceFromFloat(150.02),
				},
			},
		},
	}

	rate, err := acct.QuoteToAccount(context.Background(), "USDJPY", prices)
	require.NoError(t, err)
	// rate should be approximately 1/150 * RateScale
	approxExpected := float64(types.RateScale) / 150.01 // mid = (150.00+150.02)/2 ≈ 150.01
	assert.InDelta(t, approxExpected, float64(rate), 10)
}

// TestQuoteAccount_BaseCurrencyIsAccountCurrency_NoPrice tests the error path
// when the tick for a USD-base pair is not available.
func TestQuoteAccount_BaseCurrencyIsAccountCurrency_NoPrice(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	prices := &fakePrices{err: errors.New("no price")}

	_, err := acct.QuoteToAccount(context.Background(), "USDJPY", prices)
	assert.Error(t, err)
}

// TestQuoteAccount_UnknownInstrument ensures an unknown symbol returns an error.
func TestQuoteAccount_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	prices := &fakePrices{}

	_, err := acct.QuoteToAccount(context.Background(), "XXXYYY", prices)
	assert.Error(t, err)
}

// TestQuoteAccount_CrossCurrency tests a pair where neither base nor quote
// is the account currency.  For example, a JPY-account trading EURUSD.
func TestQuoteAccount_CrossCurrency(t *testing.T) {
	t.Parallel()

	// JPY account, EURUSD instrument:
	//   BaseCurrency=EUR, QuoteCurrency=USD – neither is JPY.
	acct := account.Account{
		ID:       "jpy",
		Currency: "JPY",
		Balance:  types.MoneyFromFloat(1_000_000),
	}
	prices := &fakePrices{}

	_, err := acct.QuoteToAccount(context.Background(), "EURUSD", prices)
	assert.Error(t, err)
}
