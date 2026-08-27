package num

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "zero", in: "0", want: "0"},
		{name: "whole shares", in: "1000", want: "1000"},
		{name: "fractional equity", in: "0.5", want: "0.5"},
		{name: "fractional crypto", in: "0.00000001", want: "0.00000001"},
		{name: "maximum representable", in: "92233720368.54775807", want: "92233720368.54775807"},

		{name: "negative rejected", in: "-1", wantErr: ErrNegative},
		{name: "malformed", in: "abc", wantErr: ErrSyntax},
		{name: "excess precision", in: "1.123456789", wantErr: ErrPrecision},
		{name: "out of range", in: "999999999999999999", wantErr: ErrRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQuantity(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, Quantity{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestMustParseQuantity(t *testing.T) {
	assert.Equal(t, "5", MustParseQuantity("5").String())
	assert.Panics(t, func() { MustParseQuantity("-1") })
}

func TestQuantityZeroValueIsValidZero(t *testing.T) {
	var q Quantity
	assert.True(t, q.IsZero())
	assert.Equal(t, "0", q.String())
}

func TestQuantityComparison(t *testing.T) {
	low := MustParseQuantity("10")
	high := MustParseQuantity("20")

	assert.Equal(t, -1, low.Cmp(high))
	assert.Equal(t, 1, high.Cmp(low))
	assert.Equal(t, 0, low.Cmp(low))
	assert.True(t, low.Equal(MustParseQuantity("10")))
	assert.False(t, low.Equal(high))
}

func TestQuantityAddSub(t *testing.T) {
	a := MustParseQuantity("100")
	b := MustParseQuantity("40")

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, "140", sum.String())

	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, "60", diff.String())
}

func TestQuantitySubRejectsNegativeResult(t *testing.T) {
	a := MustParseQuantity("10")
	b := MustParseQuantity("20")

	_, err := a.Sub(b)
	require.ErrorIs(t, err, ErrNegative)
}

func TestQuantitySubToExactlyZeroIsValid(t *testing.T) {
	a := MustParseQuantity("10")
	diff, err := a.Sub(a)
	require.NoError(t, err)
	assert.True(t, diff.IsZero())
}

func TestQuantityAddOverflow(t *testing.T) {
	// Quantity{raw: ...} is a representation-boundary test construction, not
	// a public API: num exports no raw-scaled constructor. See doc.go.
	max := Quantity{raw: math.MaxInt64}
	one := MustParseQuantity("0.00000001")

	_, err := max.Add(one)
	require.ErrorIs(t, err, ErrOverflow)
}

func TestQuantityMulRate(t *testing.T) {
	// Realistic financing-rate application: 1000 units at a 0.005 rate.
	qty := MustParseQuantity("1000")
	rate := MustParseRate("0.005")

	got, err := qty.MulRate(rate)
	require.NoError(t, err)
	assert.Equal(t, "5", got.String())
}

func TestQuantityMulRateRejectsNegativeResult(t *testing.T) {
	qty := MustParseQuantity("100")
	rate := MustParseRate("-0.5")

	_, err := qty.MulRate(rate)
	require.ErrorIs(t, err, ErrNegative)
}

func TestQuantityDiv(t *testing.T) {
	a := MustParseQuantity("100")
	b := MustParseQuantity("3")

	got, err := a.Div(b)
	require.NoError(t, err)
	assert.Equal(t, "33.33333333", got.String())
}

func TestQuantityDivByZero(t *testing.T) {
	a := MustParseQuantity("1")
	_, err := a.Div(Quantity{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

func TestQuantityDivisibleBy(t *testing.T) {
	tests := []struct {
		name string
		q    string
		step string
		want bool
	}{
		{name: "exact multiple", q: "100", step: "5", want: true},
		{name: "on the step itself", q: "0.001", step: "0.001", want: true},
		{name: "zero is divisible by anything nonzero", q: "0", step: "1", want: true},
		{name: "not a multiple", q: "101", step: "5", want: false},
		{name: "fractional increment not met", q: "1.00000001", step: "0.00000010", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := MustParseQuantity(tt.q)
			step := MustParseQuantity(tt.step)

			got, err := q.DivisibleBy(step)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQuantityDivisibleByZeroStep(t *testing.T) {
	q := MustParseQuantity("1")
	_, err := q.DivisibleBy(Quantity{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

func TestQuantityRoundDown(t *testing.T) {
	tests := []struct {
		name string
		q    string
		step string
		want string
	}{
		{name: "already an exact multiple", q: "100", step: "5", want: "100"},
		{name: "rounds down to the nearest multiple", q: "101", step: "5", want: "100"},
		{name: "just below the next multiple", q: "104.99999999", step: "5", want: "100"},
		{name: "smaller than one whole step rounds to zero", q: "0.5", step: "1", want: "0"},
		{name: "step of one is a no-op floor to whole units", q: "123.456", step: "1", want: "123"},
		{name: "q already zero", q: "0", step: "1", want: "0"},
		{name: "fractional step", q: "1.00000009", step: "0.00000010", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := MustParseQuantity(tt.q)
			step := MustParseQuantity(tt.step)

			got, err := q.RoundDown(step)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

// TestQuantityRoundDownNeverExceedsInput is the property ADR-030 and
// issue #181's own acceptance criteria actually depend on: the result
// is never greater than q, for a range of q/step combinations that are
// not exact multiples of one another.
func TestQuantityRoundDownNeverExceedsInput(t *testing.T) {
	cases := []struct{ q, step string }{
		{"1000", "3"},
		{"0.1", "0.03"},
		{"999999999", "7"},
		{"1", "0.99999999"},
	}
	for _, c := range cases {
		q := MustParseQuantity(c.q)
		step := MustParseQuantity(c.step)

		got, err := q.RoundDown(step)
		require.NoError(t, err)
		assert.LessOrEqual(t, got.Cmp(q), 0, "RoundDown(%s, %s) = %s must not exceed %s", c.q, c.step, got, q)

		divisible, err := got.DivisibleBy(step)
		require.NoError(t, err)
		assert.True(t, divisible, "RoundDown(%s, %s) = %s must be an exact multiple of %s", c.q, c.step, got, step)
	}
}

func TestQuantityRoundDownRejectsZeroStep(t *testing.T) {
	q := MustParseQuantity("1")
	_, err := q.RoundDown(Quantity{})
	require.ErrorIs(t, err, ErrDivideByZero)
}
