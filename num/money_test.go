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

// TestMoneyStringMarksInvalidMoneyVisibly guards against String() rendering
// invalid Money as something that merely looks like canonical output, such as
// "0 " for the Go zero value.
func TestMoneyStringMarksInvalidMoneyVisibly(t *testing.T) {
	var m Money
	assert.Equal(t, "<invalid money>", m.String())
	assert.NotContains(t, m.String(), " USD")
}

func TestMoneyExplicitZeroIsValid(t *testing.T) {
	m := usd("0")
	assert.True(t, m.IsValid())
	assert.True(t, m.IsZero())
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
	// Money{amount: ..., currency: ..., valid: true} is a
	// representation-boundary test construction, not a public API: num
	// exports no raw-scaled constructor. See doc.go.
	m := Money{amount: math.MinInt64, currency: MustParseCurrency("USD"), valid: true}

	_, err := m.Neg()
	require.ErrorIs(t, err, ErrNotRepresentable)

	_, err = m.Abs()
	require.ErrorIs(t, err, ErrNotRepresentable)
}

func TestMoneyOverflow(t *testing.T) {
	max := Money{amount: math.MaxInt64, currency: MustParseCurrency("USD"), valid: true}
	one := usd("0.00000001")

	_, err := max.Add(one)
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

func TestMoneyConvert(t *testing.T) {
	m := usd("100.00")
	rate := MustParseRate("0.85")

	got, err := m.Convert(MustParseCurrency("EUR"), rate)
	require.NoError(t, err)
	assert.Equal(t, "85 EUR", got.String())
	assert.Equal(t, "EUR", got.Currency().String())
	assert.Equal(t, "100 USD", m.String(), "Convert must not mutate the receiver")
}

func TestMoneyConvertToSameCurrency(t *testing.T) {
	m := usd("100.00")

	got, err := m.Convert(MustParseCurrency("USD"), MustParseRate("1.05"))
	require.NoError(t, err)
	assert.Equal(t, "105 USD", got.String())
}

func TestMoneyConvertRoundsHalfToEven(t *testing.T) {
	// Mirrors MulRate's rounding contract; Convert differs only in the
	// resulting currency.
	m := usd("0.00000005")
	rate := MustParseRate("1")

	got, err := m.Convert(MustParseCurrency("EUR"), rate)
	require.NoError(t, err)
	want, err := m.MulRate(rate)
	require.NoError(t, err)
	assert.Equal(t, want.String(), "0.00000005 USD")
	assert.Equal(t, got.String(), "0.00000005 EUR")
}

func TestMoneyConvertRejectsInvalidMoney(t *testing.T) {
	_, err := Money{}.Convert(MustParseCurrency("EUR"), MustParseRate("1"))
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneyConvertRejectsInvalidTargetCurrency(t *testing.T) {
	m := usd("100.00")
	_, err := m.Convert(Currency{}, MustParseRate("1"))
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneyConvertZeroRate(t *testing.T) {
	m := usd("100.00")
	got, err := m.Convert(MustParseCurrency("EUR"), Rate{})
	require.NoError(t, err)
	assert.True(t, got.IsZero())
	assert.Equal(t, "EUR", got.Currency().String())
}

func TestMoneyConvertNegativeRate(t *testing.T) {
	m := usd("100.00")
	got, err := m.Convert(MustParseCurrency("EUR"), MustParseRate("-1"))
	require.NoError(t, err)
	assert.Equal(t, "-100 EUR", got.String())
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

func TestMoneyDivQuantity(t *testing.T) {
	tests := []struct {
		name string
		m    string
		q    string
		want string
	}{
		{name: "exact weighted average", m: "1200", q: "1000", want: "1.2"},
		// Inverse of Price.MulQuantity's ADR-004/ADR-025 evidence cases.
		{name: "BRK.A block inverse", m: "7500000000", q: "10000", want: "750000"},
		{name: "FX 1B notional inverse", m: "1084730000", q: "1000000000", want: "1.08473"},
		{name: "zero cost basis", m: "0", q: "1000", want: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := usd(tt.m)
			q := MustParseQuantity(tt.q)

			got, err := m.DivQuantity(q)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestMoneyDivQuantityRoundsHalfToEven(t *testing.T) {
	// 1 / 200,000,000 = 0.000000005 exactly, the midpoint between
	// 0.00000000 and 0.00000001; the even neighbour is 0.00000000.
	m := usd("1")
	q := MustParseQuantity("200000000")

	got, err := m.DivQuantity(q)
	require.NoError(t, err)
	assert.Equal(t, "0", got.String())
}

func TestMoneyDivQuantityRejectsInvalidMoney(t *testing.T) {
	var m Money
	_, err := m.DivQuantity(MustParseQuantity("1"))
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestMoneyDivQuantityRejectsZeroQuantity(t *testing.T) {
	m := usd("100")
	_, err := m.DivQuantity(Quantity{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

func TestMoneyDivQuantityRejectsNegativeResult(t *testing.T) {
	negative, err := usd("100").Neg()
	require.NoError(t, err)
	_, err = negative.DivQuantity(MustParseQuantity("1"))
	require.ErrorIs(t, err, ErrNegative)
}

// TestMoneyMulQuantityDivQuantityRoundTrip confirms MulQuantity and
// DivQuantity are true inverses (up to the same rounding both already
// apply), for a range of realistic notional sizes — the same
// round-trip discipline num's other Mul/Div pairs already satisfy.
func TestMoneyMulQuantityDivQuantityRoundTrip(t *testing.T) {
	usdCode := MustParseCurrency("USD")
	cases := []struct {
		price string
		qtys  []string
	}{
		{price: "1.08473", qtys: []string{"1", "1000", "10000000"}},
		{price: "750000.00", qtys: []string{"1", "1000"}},
		{price: "150000.12345678", qtys: []string{"1", "1000"}},
	}

	for _, c := range cases {
		p := c.price
		for _, q := range c.qtys {
			t.Run(p+"_"+q, func(t *testing.T) {
				price := MustParsePrice(p)
				qty := MustParseQuantity(q)

				notional, err := price.MulQuantity(qty, usdCode)
				require.NoError(t, err)

				roundTripped, err := notional.DivQuantity(qty)
				require.NoError(t, err)
				assert.True(t, price.Equal(roundTripped), "MulQuantity then DivQuantity must recover the original price exactly for whole-scale inputs")
			})
		}
	}
}
