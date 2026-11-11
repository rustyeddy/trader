package num

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCurrencyValid(t *testing.T) {
	tests := []string{
		"USD", "EUR", "GBP", "JPY", // 3-letter FX/ISO-shaped
		"USDT", "USDC", // 4-letter crypto
		"WAVES", // 5-letter, upper bound
	}

	for _, code := range tests {
		t.Run(code, func(t *testing.T) {
			c, err := ParseCurrency(code)
			require.NoError(t, err)
			assert.Equal(t, code, c.String())
			assert.True(t, c.IsValid())
		})
	}
}

func TestParseCurrencyInvalid(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{name: "empty", code: ""},
		{name: "too short", code: "US"},
		{name: "too long", code: "TOOLONG"},
		{name: "lowercase", code: "usd"},
		{name: "mixed case", code: "Usd"},
		{name: "contains digits", code: "US1"},
		{name: "contains punctuation", code: "US-D"},
		{name: "contains underscore", code: "US_D"},
		{name: "contains whitespace", code: "US D"},
		{name: "leading whitespace", code: " USD"},
		{name: "trailing whitespace", code: "USD "},
		{name: "currency symbol", code: "$UD"},
		{name: "non-ascii letters", code: "USÐ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCurrency(tt.code)
			require.ErrorIs(t, err, ErrInvalidCurrency)
		})
	}
}

func TestMustParseCurrency(t *testing.T) {
	assert.Equal(t, "USD", MustParseCurrency("USD").String())
	assert.Panics(t, func() { MustParseCurrency("usd") })
}

func TestCurrencyZeroValueIsInvalid(t *testing.T) {
	var c Currency
	assert.False(t, c.IsValid())
	assert.Equal(t, "", c.String())
}

func TestCurrencyEqual(t *testing.T) {
	usd1 := MustParseCurrency("USD")
	usd2 := MustParseCurrency("USD")
	eur := MustParseCurrency("EUR")

	assert.True(t, usd1.Equal(usd2))
	assert.False(t, usd1.Equal(eur))

	var zero Currency
	assert.False(t, usd1.Equal(zero))
	assert.True(t, zero.Equal(Currency{}))
}
