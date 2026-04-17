package account_test

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	rate, err := acct.QuoteToAccount("EURUSD", 1013322)
	require.NoError(t, err)
	assert.Equal(t, types.Rate(types.RateScale), rate)
}

// TestQuoteAccount_BaseCurrencyIsAccountCurrency tests pairs like USDJPY for a
// USD-denominated account.  The base currency is USD == account currency, so
// the conversion rate is 1/mid.
func TestQuoteAccount_BaseCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()

	rate, err := acct.QuoteToAccount("USDJPY", types.PriceFromFloat(150.02))
	require.NoError(t, err)
	approxExpected := float64(types.RateScale) / 150.02
	assert.InDelta(t, approxExpected, float64(rate), 10)
}

// TestQuoteAccount_BaseCurrencyIsAccountCurrency_InvalidPrice tests the error path
// when no valid price is available for a USD-base pair.
func TestQuoteAccount_BaseCurrencyIsAccountCurrency_InvalidPrice(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	_, err := acct.QuoteToAccount("USDJPY", 0)
	assert.Error(t, err)
}

// TestQuoteAccount_UnknownInstrument ensures an unknown symbol returns an error.
func TestQuoteAccount_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	_, err := acct.QuoteToAccount("XXXYYY", types.PriceFromFloat(150.02))
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
	_, err := acct.QuoteToAccount("EURUSD", types.Price(1_000_000))
	assert.Error(t, err)
}
