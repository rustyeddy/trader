package num

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "zero", in: "0", want: "0"},
		{name: "positive", in: "1.05", want: "1.05"},
		{name: "negative", in: "-0.005", want: "-0.005"},
		{name: "full precision", in: "0.00000001", want: "0.00000001"},
		{name: "minimum representable", in: "-92233720368.54775808", want: "-92233720368.54775808"},
		{name: "maximum representable", in: "92233720368.54775807", want: "92233720368.54775807"},

		{name: "malformed", in: "abc", wantErr: ErrSyntax},
		{name: "empty", in: "", wantErr: ErrSyntax},
		{name: "excess precision", in: "1.123456789", wantErr: ErrPrecision},
		{name: "out of range", in: "999999999999999999", wantErr: ErrRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRate(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, Rate{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestMustParseRate(t *testing.T) {
	assert.Equal(t, "1.5", MustParseRate("1.5").String())
	assert.Panics(t, func() { MustParseRate("not a rate") })
}

func TestRateZeroValueIsValidZero(t *testing.T) {
	var r Rate
	assert.True(t, r.IsZero())
	assert.Equal(t, "0", r.String())
	assert.Equal(t, 0, r.Sign())
}

func TestRateComparison(t *testing.T) {
	low := MustParseRate("1.00")
	high := MustParseRate("2.00")

	assert.Equal(t, -1, low.Cmp(high))
	assert.Equal(t, 1, high.Cmp(low))
	assert.Equal(t, 0, low.Cmp(low))
	assert.True(t, low.Equal(MustParseRate("1.00")))
	assert.False(t, low.Equal(high))
}

func TestRateArithmetic(t *testing.T) {
	a := MustParseRate("1.5")
	b := MustParseRate("0.5")

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, "2", sum.String())

	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, "1", diff.String())

	neg, err := a.Neg()
	require.NoError(t, err)
	assert.Equal(t, "-1.5", neg.String())

	abs, err := neg.Abs()
	require.NoError(t, err)
	assert.Equal(t, "1.5", abs.String())
}

func TestRateArithmeticOverflow(t *testing.T) {
	// Rate{raw: ...} is a representation-boundary test construction, not a
	// public API: num exports no raw-scaled constructor. See doc.go.
	max := Rate{raw: math.MaxInt64}
	one := MustParseRate("0.00000001")

	_, err := max.Add(one)
	require.ErrorIs(t, err, ErrOverflow)

	min := Rate{raw: math.MinInt64}
	_, err = min.Sub(one)
	require.ErrorIs(t, err, ErrUnderflow)
}

func TestRateNegAbsMinInt64(t *testing.T) {
	min := Rate{raw: math.MinInt64}

	_, err := min.Neg()
	require.ErrorIs(t, err, ErrNotRepresentable)

	_, err = min.Abs()
	require.ErrorIs(t, err, ErrNotRepresentable)
}

func TestRateMulDiv(t *testing.T) {
	a := MustParseRate("1.05")
	b := MustParseRate("2.00")

	product, err := a.MulRate(b)
	require.NoError(t, err)
	assert.Equal(t, "2.1", product.String())

	quotient, err := b.DivRate(a)
	require.NoError(t, err)
	assert.Equal(t, "1.9047619", quotient.String())
}

func TestRateDivByZero(t *testing.T) {
	a := MustParseRate("1.00")
	_, err := a.DivRate(Rate{})
	require.ErrorIs(t, err, ErrDivideByZero)
}
