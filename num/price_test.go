package num

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePrice(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "zero", in: "0", want: "0"},
		{name: "typical FX price", in: "1.08473", want: "1.08473"},
		{name: "typical equity price", in: "123.45", want: "123.45"},
		{name: "high priced instrument", in: "92233720368.54775807", want: "92233720368.54775807"},
		{name: "one quantum", in: "0.00000001", want: "0.00000001"},

		{name: "negative rejected", in: "-1.00", wantErr: ErrNegative},
		{name: "negative quantum rejected", in: "-0.00000001", wantErr: ErrNegative},
		{name: "malformed", in: "abc", wantErr: ErrSyntax},
		{name: "excess precision", in: "1.123456789", wantErr: ErrPrecision},
		{name: "out of range", in: "999999999999999999", wantErr: ErrRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePrice(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, Price{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestMustParsePrice(t *testing.T) {
	assert.Equal(t, "1.5", MustParsePrice("1.5").String())
	assert.Panics(t, func() { MustParsePrice("-1") })
}

func TestPriceZeroValueIsValidZero(t *testing.T) {
	var p Price
	assert.True(t, p.IsZero())
	assert.Equal(t, "0", p.String())
}

func TestPriceComparison(t *testing.T) {
	low := MustParsePrice("1.00")
	high := MustParsePrice("2.00")

	assert.Equal(t, -1, low.Cmp(high))
	assert.Equal(t, 1, high.Cmp(low))
	assert.Equal(t, 0, low.Cmp(low))
	assert.True(t, low.Equal(MustParsePrice("1.00")))
	assert.False(t, low.Equal(high))
}

func TestPriceAddSub(t *testing.T) {
	a := MustParsePrice("1.50")
	b := MustParsePrice("0.50")

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, "2", sum.String())

	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, "1", diff.String())
}

func TestPriceSubRejectsNegativeResult(t *testing.T) {
	a := MustParsePrice("1.00")
	b := MustParsePrice("2.00")

	_, err := a.Sub(b)
	require.ErrorIs(t, err, ErrNegative)
}

func TestPriceSubExactlyZeroIsValid(t *testing.T) {
	a := MustParsePrice("1.00")
	diff, err := a.Sub(a)
	require.NoError(t, err)
	assert.True(t, diff.IsZero())
}

func TestPriceAddOverflow(t *testing.T) {
	// Price{raw: ...} is a representation-boundary test construction, not a
	// public API: num exports no raw-scaled constructor. See doc.go.
	max := Price{raw: math.MaxInt64}
	one := MustParsePrice("0.00000001")

	_, err := max.Add(one)
	require.ErrorIs(t, err, ErrOverflow)
}

func TestPriceMulRate(t *testing.T) {
	// Realistic markup: a 1.08473 price scaled by a 1.05 rate.
	price := MustParsePrice("1.08473")
	rate := MustParseRate("1.05")

	got, err := price.MulRate(rate)
	require.NoError(t, err)
	assert.Equal(t, "1.1389665", got.String())
}

func TestPriceMulRateRejectsNegativeResult(t *testing.T) {
	price := MustParsePrice("1.00")
	rate := MustParseRate("-0.5")

	_, err := price.MulRate(rate)
	require.ErrorIs(t, err, ErrNegative)
}

func TestPriceDiv(t *testing.T) {
	a := MustParsePrice("1.08473")
	b := MustParsePrice("1.08000")

	got, err := a.Div(b)
	require.NoError(t, err)
	assert.Equal(t, "1.00437963", got.String())
}

func TestPriceDivByZero(t *testing.T) {
	a := MustParsePrice("1.00")
	_, err := a.Div(Price{})
	require.ErrorIs(t, err, ErrDivideByZero)
}
