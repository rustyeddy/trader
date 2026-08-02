package numericstudy

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoundTrip is the core evidence for #33: every representative asset
// price, at every candidate scale, either round-trips exactly or fails for a
// documented reason.  A scale that cannot hold a value we intend to support is
// disqualified, and the subtest name says which pair failed.
func TestRoundTrip(t *testing.T) {
	for _, sc := range Candidates {
		for _, a := range Assets {
			t.Run(sc.Name+"/"+a.Symbol, func(t *testing.T) {
				v, err := ParseDecimal(a.Price, sc)
				if a.Decimals > sc.Decimals {
					require.ErrorIs(t, err, ErrTooManyDecimals,
						"%s needs %d decimals; scale %s holds %d",
						a.Symbol, a.Decimals, sc.Name, sc.Decimals)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, Canonical(a.Price), FormatDecimal(v, sc),
					"round trip must be exact")
			})
		}
	}
}

// TestTickSizeRepresentable checks that instrument tick sizes parse exactly.
// Tick size is an instrument rule, not the representation scale, but a scale
// that cannot express a tick cannot validate price increments either.
func TestTickSizeRepresentable(t *testing.T) {
	for _, sc := range Candidates {
		for _, a := range Assets {
			t.Run(sc.Name+"/"+a.Symbol, func(t *testing.T) {
				v, err := ParseDecimal(a.TickSize, sc)
				if a.Decimals > sc.Decimals {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, Canonical(a.TickSize), FormatDecimal(v, sc))
				assert.Positive(t, v, "tick size must be non-zero at this scale")
			})
		}
	}
}

// TestPriceIsMultipleOfTick confirms the separation of concerns: with prices
// and ticks in the same internal scale, "is this price on a valid increment?"
// is exact integer modulo, with no scale-specific special cases.
func TestPriceIsMultipleOfTick(t *testing.T) {
	sc := scaleByName(t, "1e9")
	for _, a := range Assets {
		t.Run(a.Symbol, func(t *testing.T) {
			p, err := ParseDecimal(a.Price, sc)
			require.NoError(t, err)
			tick, err := ParseDecimal(a.TickSize, sc)
			require.NoError(t, err)
			assert.Zero(t, p%tick, "%s price must sit on a tick boundary", a.Symbol)
		})
	}
}

// TestRangeHeadroom documents the maximum whole price at each scale and
// asserts the highest-priced asset we intend to support still fits with room
// to spare.
func TestRangeHeadroom(t *testing.T) {
	want := map[string]int64{
		"1e5": 92_233_720_368_547,
		"1e6": 9_223_372_036_854,
		"1e8": 92_233_720_368,
		"1e9": 9_223_372_036,
	}
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			require.Equal(t, want[sc.Name], MaxPrice(sc))

			if sc.Decimals < 2 {
				return
			}
			v, err := ParseDecimal("750000.00", sc)
			require.NoError(t, err, "BRK.A-class price must be representable")
			assert.Greater(t, Headroom(v, sc), int64(1_000),
				"want at least 1000x headroom above the highest-priced asset")
		})
	}
}

// TestMaxBoundary walks the exact edge of representability at each scale.
func TestMaxBoundary(t *testing.T) {
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			max := MaxPriceText(sc)

			v, err := ParseDecimal(max, sc)
			require.NoError(t, err)
			assert.Equal(t, max, FormatDecimal(v, sc))

			past := strconv.FormatInt(MaxPrice(sc)+1, 10)
			_, err = ParseDecimal(past, sc)
			assert.ErrorIs(t, err, ErrOverflow, "one whole unit past max must overflow")
		})
	}
}

// TestSmallestRepresentable checks the other end of the range: one scale unit.
func TestSmallestRepresentable(t *testing.T) {
	cases := map[string]string{
		"1e5": "0.00001",
		"1e6": "0.000001",
		"1e8": "0.00000001",
		"1e9": "0.000000001",
	}
	for _, sc := range Candidates {
		t.Run(sc.Name, func(t *testing.T) {
			text := cases[sc.Name]
			v, err := ParseDecimal(text, sc)
			require.NoError(t, err)
			assert.Equal(t, int64(1), v)
			assert.Equal(t, text, FormatDecimal(v, sc))

			// Half a unit is not representable and must be rejected, not
			// silently rounded to 0 or 1.
			_, err = ParseDecimal(text+"5", sc)
			assert.ErrorIs(t, err, ErrTooManyDecimals)
		})
	}
}

func TestParseRejects(t *testing.T) {
	sc := scaleByName(t, "1e9")
	cases := []struct {
		name, in string
		wantErr  error
	}{
		{"empty", "", ErrSyntax},
		{"bare sign", "-", ErrSyntax},
		{"leading plus", "+1.5", ErrSyntax},
		{"double dot", "1.2.3", ErrSyntax},
		{"trailing dot", "1.", ErrSyntax},
		{"leading dot", ".5", ErrSyntax},
		{"space", " 1.5", ErrSyntax},
		{"trailing space", "1.5 ", ErrSyntax},
		{"grouped", "750,000.00", ErrSyntax},
		{"exponent", "1.5e-8", ErrSyntax},
		{"treasury dash", "110-16", ErrSyntax},
		{"grain apostrophe", "575'6", ErrSyntax},
		{"letters", "abc", ErrSyntax},
		{"nan", "NaN", ErrSyntax},
		{"inf", "Inf", ErrSyntax},
		{"too many decimals", "1.0000000001", ErrTooManyDecimals},
		{"overflow", "99999999999.0", ErrOverflow},
		{"negative overflow", "-99999999999.0", ErrOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseDecimal(c.in, sc)
			assert.ErrorIs(t, err, c.wantErr)
		})
	}
}

func TestParseAccepts(t *testing.T) {
	sc := scaleByName(t, "1e9")
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"-0", 0},
		{"0.0", 0},
		{"-0.0", 0},
		{"1", 1_000_000_000},
		{"-1", -1_000_000_000},
		{"000123.4500", 123_450_000_000},
		{"1.08473", 1_084_730_000},
		{"-1.08473", -1_084_730_000},
		{"0.000000001", 1},
		{"-0.000000001", -1},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			v, err := ParseDecimal(c.in, sc)
			require.NoError(t, err)
			assert.Equal(t, c.want, v)
		})
	}
}

// TestFormatNegativeAndZero pins the sign and zero handling that a naive
// int64 division would get wrong.
func TestFormatNegativeAndZero(t *testing.T) {
	sc := scaleByName(t, "1e9")
	cases := []struct {
		v    int64
		want string
	}{
		{0, "0"},
		{1, "0.000000001"},
		{-1, "-0.000000001"},
		{-500_000_000, "-0.5"},
		{-1_500_000_000, "-1.5"},
		{math.MaxInt64, "9223372036.854775807"},
		{math.MinInt64, "-9223372036.854775808"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			assert.Equal(t, c.want, FormatDecimal(c.v, sc))
		})
	}
}

// TestFormatFixed demonstrates that display precision is chosen per
// instrument and is independent of the internal scale.
func TestFormatFixed(t *testing.T) {
	sc := scaleByName(t, "1e9")
	v, err := ParseDecimal("1.08473", sc)
	require.NoError(t, err)

	assert.Equal(t, "1", FormatFixed(v, sc, 0))
	assert.Equal(t, "1.08", FormatFixed(v, sc, 2))
	assert.Equal(t, "1.08473", FormatFixed(v, sc, 5))
	assert.Equal(t, "1.084730000000", FormatFixed(v, sc, 12))
	assert.Equal(t, "-1.08473", FormatFixed(-v, sc, 5))
}

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"750000.00":  "750000",
		"1.08473":    "1.08473",
		"0.00000001": "0.00000001",
		"000123.450": "123.45",
		"-0.0":       "0",
		"-1.500":     "-1.5",
		"0":          "0",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, Canonical(in))
		})
	}
}

// TestUnsupportedQuotationsAreRejected proves the documented unsupported
// formats really do fail at the parse boundary, and that the decimal each one
// normalizes to is representable once an adapter has converted it.
func TestUnsupportedQuotationsAreRejected(t *testing.T) {
	sc := scaleByName(t, "1e9")
	for _, q := range UnsupportedQuotations {
		t.Run(q.Format, func(t *testing.T) {
			_, err := ParseDecimal(q.Example, sc)
			require.Error(t, err, "raw quotation must not parse")

			v, err := ParseDecimal(q.Decimal, sc)
			require.NoError(t, err, "normalized decimal must parse")
			assert.Equal(t, Canonical(q.Decimal), FormatDecimal(v, sc))
		})
	}
}

func TestMulOverflows(t *testing.T) {
	assert.False(t, MulOverflows(0, math.MaxInt64))
	assert.False(t, MulOverflows(math.MaxInt64, 0))
	assert.False(t, MulOverflows(math.MaxInt64, 1))
	assert.False(t, MulOverflows(-1, math.MaxInt64))
	assert.True(t, MulOverflows(math.MaxInt64, 2))
	assert.True(t, MulOverflows(math.MinInt64, 1), "MinInt64 has no positive twin")
	assert.True(t, MulOverflows(1<<32, 1<<32))
}

func TestAddOverflows(t *testing.T) {
	assert.False(t, AddOverflows(math.MaxInt64, 0))
	assert.False(t, AddOverflows(math.MaxInt64-1, 1))
	assert.True(t, AddOverflows(math.MaxInt64, 1))
	assert.False(t, AddOverflows(math.MinInt64+1, -1))
	assert.True(t, AddOverflows(math.MinInt64, -1))
}

func scaleByName(t *testing.T, name string) Scale {
	t.Helper()
	for _, sc := range Candidates {
		if sc.Name == name {
			return sc
		}
	}
	t.Fatalf("unknown scale %q", name)
	return Scale{}
}
