package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usdAccount() Account {
	return Account{
		ID:       "test",
		Currency: "USD",
		Balance:  MoneyFromFloat(100_000),
		Equity:   MoneyFromFloat(100_000),
	}
}

func TestQuoteToRate_QuoteCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()
	rate, err := acct.QuoteToAccount("EURUSD", 1013322)
	require.NoError(t, err)
	assert.Equal(t, Rate(rateScale), rate)
}

func TestQuoteAccount_BaseCurrencyIsAccountCurrency(t *testing.T) {
	t.Parallel()

	acct := usdAccount()

	rate, err := acct.QuoteToAccount("USDJPY", PriceFromFloat(150.02))
	require.NoError(t, err)
	approxExpected := float64(rateScale) / 150.02
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
	_, err := acct.QuoteToAccount("XXXYYY", PriceFromFloat(150.02))
	assert.Error(t, err)
}

func TestQuoteAccount_CrossCurrency(t *testing.T) {
	t.Parallel()

	acct := Account{
		ID:       "jpy",
		Currency: "JPY",
		Balance:  MoneyFromFloat(1_000_000),
	}
	_, err := acct.QuoteToAccount("EURUSD", Price(1_000_000))
	assert.Error(t, err)
}
