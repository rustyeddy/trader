package trader

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usdAccount() Account {
	return Account{
		ID:       "test",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(100_000),
		Equity:   types.MoneyFromFloat(100_000),
	}
}

func TestQuoteToRate_QuoteCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	rate, err := acct.QuoteToAccount("EURUSD", 1013322)
	require.NoError(t, err)
	assert.Equal(t, types.Rate(types.RateScale), rate)
}

func TestQuoteAccount_BaseCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()

	rate, err := acct.QuoteToAccount("USDJPY", types.PriceFromFloat(150.02))
	require.NoError(t, err)
	approxExpected := float64(types.RateScale) / 150.02
	assert.InDelta(t, approxExpected, float64(rate), 10)
}

func TestQuoteAccount_BaseCurrencyIsAccountCurrency_InvalidPrice(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	_, err := acct.QuoteToAccount("USDJPY", 0)
	assert.Error(t, err)
}

func TestQuoteAccount_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	_, err := acct.QuoteToAccount("XXXYYY", types.PriceFromFloat(150.02))
	assert.Error(t, err)
}

func TestQuoteAccount_CrossCurrency(t *testing.T) {
	t.Parallel()

	acct := Account{
		ID:       "jpy",
		Currency: "JPY",
		Balance:  types.MoneyFromFloat(1_000_000),
	}
	_, err := acct.QuoteToAccount("EURUSD", types.Price(1_000_000))
	assert.Error(t, err)
}
