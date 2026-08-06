package num

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usd(s string) Money {
	return MustParseMoney(s, MustParseCurrency("USD"))
}

func eur(s string) Money {
	return MustParseMoney(s, MustParseCurrency("EUR"))
}

func TestParseMoneyValid(t *testing.T) {
	tests := []struct {
		name       string
		amount     string
		currency   string
		wantString string
	}{
		{name: "positive", amount: "123.45", currency: "USD", wantString: "123.45 USD"},
		{name: "negative", amount: "-50.00", currency: "EUR", wantString: "-50 EUR"},
		{name: "explicit zero", amount: "0", currency: "USD", wantString: "0 USD"},
		{name: "full precision", amount: "0.00000001", currency: "BTC", wantString: "0.00000001 BTC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseMoney(tt.amount, MustParseCurrency(tt.currency))
			require.NoError(t, err)
			assert.True(t, m.IsValid())
			assert.Equal(t, tt.wantString, m.String())
		})
	}
}

func TestParseMoneyRejectsInvalidCurrency(t *testing.T) {
	_, err := ParseMoney("100.00", Currency{})
	require.ErrorIs(t, err, ErrMissingCurrency)

	_, err = ParseMoney("100.00", Currency{code: "usd"})
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestParseMoneyRejectsMalformedAmount(t *testing.T) {
	usdCode := MustParseCurrency("USD")

	_, err := ParseMoney("abc", usdCode)
	require.ErrorIs(t, err, ErrSyntax)

	_, err = ParseMoney("1.123456789", usdCode)
	require.ErrorIs(t, err, ErrPrecision)
}

func TestMoneyZeroValueIsInvalid(t *testing.T) {
	var m Money
	assert.False(t, m.IsValid())
	assert.False(t, m.Currency().IsValid())
}

func TestMoneyExplicitZeroIsValid(t *testing.T) {
	m := usd("0")
	assert.True(t, m.IsValid())
	assert.True(t, m.IsZero())
}

func TestMoneyFromScaled(t *testing.T) {
	m, err := MoneyFromScaled(12_345_000_000, MustParseCurrency("USD"))
	require.NoError(t, err)
	assert.Equal(t, "123.45 USD", m.String())

	_, err = MoneyFromScaled(100, Currency{})
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneySameCurrencyArithmetic(t *testing.T) {
	a := usd("100.00")
	b := usd("40.00")

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, "140 USD", sum.String())

	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, "60 USD", diff.String())
}

func TestMoneySubCanGoNegative(t *testing.T) {
	// Unlike Price and Quantity, Money is signed: a fee or realized loss is
	// ordinary negative money, not an error.
	a := usd("10.00")
	b := usd("25.00")

	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, "-15 USD", diff.String())
}

func TestMoneyCrossCurrencyArithmeticRejected(t *testing.T) {
	a := usd("100.00")
	b := eur("100.00")

	_, err := a.Add(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)

	_, err = a.Sub(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)

	_, err = a.Div(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)

	_, err = a.Cmp(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
}

func TestMoneyArithmeticWithInvalidOperandRejected(t *testing.T) {
	var zero Money
	a := usd("1.00")

	_, err := a.Add(zero)
	require.ErrorIs(t, err, ErrMissingCurrency)

	_, err = zero.Add(a)
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneyEqual(t *testing.T) {
	a := usd("100.00")
	b := usd("100.00")
	c := usd("100.01")
	d := eur("100.00")

	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))
	assert.False(t, a.Equal(d), "different currencies are never equal")

	var zero Money
	assert.False(t, zero.Equal(zero), "the invalid zero value is never equal to itself")
}

func TestMoneyCmp(t *testing.T) {
	low := usd("10.00")
	high := usd("20.00")

	got, err := low.Cmp(high)
	require.NoError(t, err)
	assert.Equal(t, -1, got)

	got, err = high.Cmp(low)
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	got, err = low.Cmp(low)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestMoneyNegAbs(t *testing.T) {
	a := usd("15.00")

	neg, err := a.Neg()
	require.NoError(t, err)
	assert.Equal(t, "-15 USD", neg.String())

	abs, err := neg.Abs()
	require.NoError(t, err)
	assert.Equal(t, "15 USD", abs.String())
}

func TestMoneyNegAbsMinInt64(t *testing.T) {
	m, err := MoneyFromScaled(math.MinInt64, MustParseCurrency("USD"))
	require.NoError(t, err)

	_, err = m.Neg()
	require.ErrorIs(t, err, ErrNotRepresentable)

	_, err = m.Abs()
	require.ErrorIs(t, err, ErrNotRepresentable)
}

func TestMoneyOverflow(t *testing.T) {
	max, err := MoneyFromScaled(math.MaxInt64, MustParseCurrency("USD"))
	require.NoError(t, err)
	one := usd("0.00000001")

	_, err = max.Add(one)
	require.ErrorIs(t, err, ErrOverflow)
}

func TestMoneyMulRate(t *testing.T) {
	// A 1.05 markup preserves the existing currency; it is not conversion.
	m := usd("100.00")
	rate := MustParseRate("1.05")

	got, err := m.MulRate(rate)
	require.NoError(t, err)
	assert.Equal(t, "105 USD", got.String())
	assert.Equal(t, "USD", got.Currency().String())
}

func TestMoneyDivRate(t *testing.T) {
	m := usd("105.00")
	rate := MustParseRate("1.05")

	got, err := m.DivRate(rate)
	require.NoError(t, err)
	assert.Equal(t, "100 USD", got.String())
}

func TestMoneyDivRateByZero(t *testing.T) {
	m := usd("100.00")
	_, err := m.DivRate(Rate{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

func TestMoneyDivSameCurrency(t *testing.T) {
	a := usd("50.00")
	b := usd("200.00")

	got, err := a.Div(b)
	require.NoError(t, err)
	assert.Equal(t, "0.25", got.String())
}

func TestMoneyDivByZero(t *testing.T) {
	a := usd("50.00")
	_, err := a.Div(usd("0"))
	require.ErrorIs(t, err, ErrDivideByZero)
}
