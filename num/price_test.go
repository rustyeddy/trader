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

func TestPriceMulQuantity(t *testing.T) {
	usd := MustParseCurrency("USD")

	tests := []struct {
		name string
		p    string
		q    string
		want string
	}{
		{name: "zero price", p: "0", q: "10000", want: "0"},
		{name: "zero quantity", p: "1.08473", q: "0", want: "0"},
		{name: "typical FX lot", p: "1.08473", q: "10000", want: "10847.3"},
		// ADR-004/ADR-025 evidence cases: naive double-scaled int64
		// multiplication overflows at every candidate scale for these,
		// but the widened intermediate MulQuantity uses does not.
		{name: "BRK.A block", p: "750000.00", q: "10000", want: "7500000000"},
		{name: "FX 10M notional", p: "1.08473", q: "10000000", want: "10847300"},
		{name: "FX 1B notional", p: "1.08473", q: "1000000000", want: "1084730000"},
		{name: "BTC 1k coins", p: "150000.12345678", q: "1000", want: "150000123.45678"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := MustParsePrice(tt.p)
			q := MustParseQuantity(tt.q)

			got, err := p.MulQuantity(q, usd)
			require.NoError(t, err)
			assert.Equal(t, tt.want+" USD", got.String())
			assert.True(t, got.Currency().Equal(usd))
		})
	}
}

func TestPriceMulQuantityRoundsHalfToEven(t *testing.T) {
	// 0.000000005 is not representable exactly, so use a price/quantity
	// pair whose true product lands exactly on that half-unit at the
	// eighth decimal place: 0.00000001 * 0.5 = 0.000000005, the exact
	// midpoint between 0.00000000 and 0.00000001. The even neighbour is
	// 0.00000000.
	p := MustParsePrice("0.00000001")
	q := MustParseQuantity("0.5")
	usd := MustParseCurrency("USD")

	got, err := p.MulQuantity(q, usd)
	require.NoError(t, err)
	assert.Equal(t, "0 USD", got.String())
}

func TestPriceMulQuantityRejectsInvalidCurrency(t *testing.T) {
	p := MustParsePrice("1.00")
	q := MustParseQuantity("1")

	_, err := p.MulQuantity(q, Currency{})
	require.ErrorIs(t, err, ErrMissingCurrency)
}

func TestPriceMulQuantityReportsOverflow(t *testing.T) {
	// Near the maximum representable value, multiplied by a quantity
	// large enough to push the true product past it.
	p := MustParsePrice("92233720368")
	q := MustParseQuantity("2")
	usd := MustParseCurrency("USD")

	_, err := p.MulQuantity(q, usd)
	require.ErrorIs(t, err, ErrOverflow)
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

func TestPriceDivisibleBy(t *testing.T) {
	tests := []struct {
		name string
		p    string
		step string
		want bool
	}{
		{name: "exact multiple", p: "1.08450", step: "0.00010", want: true},
		{name: "on the step itself", p: "0.25", step: "0.25", want: true},
		{name: "zero is divisible by anything nonzero", p: "0", step: "0.01", want: true},
		{name: "not a multiple", p: "1.08453", step: "0.00010", want: false},
		{name: "one quantum short", p: "1.00000001", step: "0.00000010", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := MustParsePrice(tt.p)
			step := MustParsePrice(tt.step)

			got, err := p.DivisibleBy(step)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPriceDivisibleByZeroStep(t *testing.T) {
	p := MustParsePrice("1.00")
	_, err := p.DivisibleBy(Price{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

// TestPriceRoundDown covers issue #340's own motivating case (a
// trailing-stop computation landing on a sub-cent value against a
// real SPY price) alongside ordinary exact-multiple and boundary
// cases.
func TestPriceRoundDown(t *testing.T) {
	tests := []struct {
		name string
		p    string
		tick string
		want string
	}{
		{name: "issue #340's own motivating case", p: "368.964", tick: "0.01", want: "368.96"},
		{name: "already an exact multiple", p: "1.25", tick: "0.25", want: "1.25"},
		{name: "zero rounds to zero", p: "0", tick: "0.01", want: "0"},
		{name: "less than one tick rounds to zero", p: "0.005", tick: "0.01", want: "0"},
		{name: "fine-grained FX tick", p: "1.08453", tick: "0.00010", want: "1.08450"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := MustParsePrice(tt.p)
			tick := MustParsePrice(tt.tick)
			got, err := p.RoundDown(tick)
			require.NoError(t, err)
			assert.True(t, got.Equal(MustParsePrice(tt.want)), "got %s, want %s", got, tt.want)
			ok, err := got.DivisibleBy(tick)
			require.NoError(t, err)
			assert.True(t, ok, "result must itself be tick-aligned")
		})
	}
}

func TestPriceRoundDownZeroTick(t *testing.T) {
	p := MustParsePrice("1.00")
	_, err := p.RoundDown(Price{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

// TestPriceRoundUp is RoundDown's mirror image, rounding toward
// positive infinity instead of toward zero.
func TestPriceRoundUp(t *testing.T) {
	tests := []struct {
		name string
		p    string
		tick string
		want string
	}{
		{name: "sub-cent value rounds up to the next cent", p: "368.964", tick: "0.01", want: "368.97"},
		{name: "already an exact multiple", p: "1.25", tick: "0.25", want: "1.25"},
		{name: "zero rounds to zero", p: "0", tick: "0.01", want: "0"},
		{name: "less than one tick rounds up to one tick", p: "0.005", tick: "0.01", want: "0.01"},
		{name: "fine-grained FX tick", p: "1.08453", tick: "0.00010", want: "1.08460"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := MustParsePrice(tt.p)
			tick := MustParsePrice(tt.tick)
			got, err := p.RoundUp(tick)
			require.NoError(t, err)
			assert.True(t, got.Equal(MustParsePrice(tt.want)), "got %s, want %s", got, tt.want)
			ok, err := got.DivisibleBy(tick)
			require.NoError(t, err)
			assert.True(t, ok, "result must itself be tick-aligned")
		})
	}
}

func TestPriceRoundUpZeroTick(t *testing.T) {
	p := MustParsePrice("1.00")
	_, err := p.RoundUp(Price{})
	require.ErrorIs(t, err, ErrDivideByZero)
}

// TestPriceRoundUp_ReportsOverflowInsteadOfWrapping is PR #341
// review's own finding: a p close enough to Price's maximum
// representable raw value, with a fractional remainder against tick,
// must report ErrOverflow rather than silently wrapping negative when
// rounding up would exceed int64's range.
func TestPriceRoundUp_ReportsOverflowInsteadOfWrapping(t *testing.T) {
	tick := MustParsePrice("0.01")
	// One raw unit short of a tick below the maximum representable
	// value, with a nonzero remainder against a one-cent tick: RoundUp
	// must add one more tick and overflow, not wrap.
	p := Price{raw: math.MaxInt64 - 1}
	_, err := p.RoundUp(tick)
	require.ErrorIs(t, err, ErrOverflow)
}

// TestPriceFloat64 proves Float64 (ADR-045) converts to the same
// float64 value Go's own compiler produces for the identical decimal
// literal — the correctly-rounded float64 nearest that decimal value,
// not merely "close" to it. want is deliberately an exact Go float64
// literal (not a re-parsed string), so the compiler performs the same
// decimal-to-binary rounding a caller comparing against a literal
// constant would see, and assert.Equal checks bit-for-bit equality,
// not an approximate delta: several of these inputs (1.1004, the
// 1e-8 case) are not exactly representable in binary float64 at all,
// so the property under test is "correctly rounded to the nearest
// representable value," not "exact" (PR #275 review).
func TestPriceFloat64(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want float64
	}{
		{"zero", "0", 0},
		{"whole number", "1", 1},
		{"half", "1.5", 1.5},
		{"typical FX price", "1.10040", 1.1004},
		{"smallest representable increment", "0.00000001", 0.00000001},
		{"largest representable value", "92233720368.54775807", 92233720368.54775807},
		// raw = 4888940765280053789, well above 2^53 (~9.007e15): the
		// naive float64(raw)/float64(scale) conversion rounds the raw
		// int64 once before dividing, then rounds again, and for this
		// exact value that lands on 48889407652.800545 — one ULP away
		// from 48889407652.80054, the correctly-rounded result the
		// whole/frac decomposition (and Go's own decimal-literal
		// rounding) produce. This is the PR #275 review's own example.
		{"large value above the float64 exact-integer range (2^53)", "48889407652.80053789", 48889407652.80054},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := MustParsePrice(tt.in)
			assert.Equal(t, tt.want, p.Float64())
		})
	}
}

// TestPriceFloat64ZeroValue proves the zero value converts to exactly
// 0, matching Price's own documented "zero value is the exact value 0
// and is valid" contract.
func TestPriceFloat64ZeroValue(t *testing.T) {
	var p Price
	assert.Equal(t, float64(0), p.Float64())
}

func TestParsePriceFixed(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		decimals int
		want     string
		wantErr  error
	}{
		{name: "five decimals", in: "1.08473", decimals: 5, want: "1.08473"},
		{name: "trailing zeros are kept as given", in: "1.10000", decimals: 5, want: "1.1"},
		{name: "six decimals", in: "1.104370", decimals: 6, want: "1.10437"},
		{name: "high price", in: "9999.00000", decimals: 5, want: "9999"},
		{name: "zero", in: "0.00000", decimals: 5, want: "0"},
		{name: "no decimals", in: "123", decimals: 0, want: "123"},
		{name: "eight decimals", in: "0.00000001", decimals: 8, want: "0.00000001"},

		{name: "too few decimals", in: "1.1", decimals: 5, wantErr: ErrSyntax},
		{name: "too many decimals", in: "1.123456", decimals: 5, wantErr: ErrSyntax},
		{name: "no point", in: "1", decimals: 5, wantErr: ErrSyntax},
		{name: "point where none wanted", in: "1.0", decimals: 0, wantErr: ErrSyntax},
		{name: "empty whole part", in: ".12345", decimals: 5, wantErr: ErrSyntax},
		{name: "empty", in: "", decimals: 5, wantErr: ErrSyntax},
		{name: "sign", in: "+1.10000", decimals: 5, wantErr: ErrSyntax},
		{name: "minus", in: "-1.10000", decimals: 5, wantErr: ErrSyntax},
		{name: "letters", in: "1.1000a", decimals: 5, wantErr: ErrSyntax},
		{name: "exponent", in: "1e3.00000", decimals: 5, wantErr: ErrSyntax},
		{name: "space", in: " 1.10000", decimals: 5, wantErr: ErrSyntax},
		{name: "two points", in: "1.1.1000", decimals: 5, wantErr: ErrSyntax},
		{name: "negative decimals", in: "1", decimals: -1, wantErr: ErrSyntax},
		{name: "too many decimals asked", in: "1.123456789", decimals: 9, wantErr: ErrSyntax},
		{name: "out of range", in: "999999999999999999.00000", decimals: 5, wantErr: ErrRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePriceFixed(tt.in, tt.decimals)
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

func TestPriceFormatFixed(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		decimals int
		want     string
		wantErr  error
	}{
		{name: "pads with zeros", in: "1.1", decimals: 5, want: "1.10000"},
		{name: "exact", in: "1.10437", decimals: 5, want: "1.10437"},
		{name: "whole number", in: "10", decimals: 5, want: "10.00000"},
		{name: "zero", in: "0", decimals: 5, want: "0.00000"},
		{name: "smallest tick", in: "0.00005", decimals: 5, want: "0.00005"},
		{name: "six decimals", in: "1.100055", decimals: 6, want: "1.100055"},
		{name: "no decimals", in: "123", decimals: 0, want: "123"},
		{name: "eight decimals", in: "0.00000001", decimals: 8, want: "0.00000001"},
		{name: "high price", in: "92233720368.54775807", decimals: 8, want: "92233720368.54775807"},

		{name: "more precision is refused, not rounded", in: "1.123456", decimals: 5, wantErr: ErrPrecision},
		{name: "fraction with no decimals", in: "1.5", decimals: 0, wantErr: ErrPrecision},
		{name: "negative decimals", in: "1", decimals: -1, wantErr: ErrPrecision},
		{name: "too many decimals asked", in: "1", decimals: 9, wantErr: ErrPrecision},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MustParsePrice(tt.in).FormatFixed(tt.decimals)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatFixedAndParsePriceFixedAreInverse(t *testing.T) {
	for _, in := range []string{"0", "0.00001", "1.10437", "9999.99999", "123.45"} {
		p := MustParsePrice(in)

		text, err := p.FormatFixed(5)
		require.NoError(t, err)
		back, err := ParsePriceFixed(text, 5)

		require.NoError(t, err, in)
		assert.Equal(t, p, back, in)
	}
}
