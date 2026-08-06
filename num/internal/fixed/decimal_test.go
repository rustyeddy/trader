package fixed

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		{name: "zero", in: "0", want: 0},
		{name: "signed zero is normalized", in: "-0", want: 0},
		{name: "explicitly positive zero", in: "+0", want: 0},
		{name: "zero with a zero fraction", in: "0.00000000", want: 0},
		{name: "negative zero fraction is normalized", in: "-0.00000000", want: 0},

		{name: "whole number", in: "1", want: scale},
		{name: "negative whole number", in: "-1", want: -scale},
		{name: "explicit plus sign", in: "+1", want: scale},
		{name: "leading zeros are accepted", in: "007", want: 7 * scale},

		{name: "one decimal place", in: "0.5", want: 50_000_000},
		{name: "two decimal places", in: "123.45", want: 12_345_000_000},
		{name: "five decimal places", in: "1.08473", want: 108_473_000},
		{name: "all eight decimal places", in: "1.12345678", want: 112_345_678},
		{name: "one quantum", in: "0.00000001", want: 1},
		{name: "one negative quantum", in: "-0.00000001", want: -1},
		{name: "smallest fraction digit in each place", in: "0.00000009", want: 9},

		{name: "trailing zeros are accepted", in: "1.00000000", want: scale},
		{name: "trailing zeros within the scale", in: "123.45000000", want: 12_345_000_000},
		// Digits beyond the scale are accepted only when dropping them is
		// exact; no rounding is involved.
		{name: "zeros beyond the scale are exact", in: "1.000000000000", want: scale},
		{name: "many zeros beyond the scale", in: "0.5000000000000000000", want: 50_000_000},

		{name: "maximum representable value", in: "92233720368.54775807", want: maxRaw},
		{name: "minimum representable value", in: "-92233720368.54775808", want: minRaw},
		{name: "one above the minimum", in: "-92233720368.54775807", want: minRaw + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseFullNegativeRange guards the one-value gap that appears when an
// implementation parses the magnitude into a signed integer and only then
// applies the sign: math.MinInt64's positive counterpart is not representable,
// so that approach rejects the most negative value it is required to accept.
func TestParseFullNegativeRange(t *testing.T) {
	got, err := Parse("-92233720368.54775808")
	require.NoError(t, err)
	assert.Equal(t, int64(math.MinInt64), got)

	// Its positive counterpart is genuinely out of range.
	_, err = Parse("92233720368.54775808")
	require.ErrorIs(t, err, ErrRange)
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr error
	}{
		{name: "empty", in: "", wantErr: ErrSyntax},
		{name: "bare minus", in: "-", wantErr: ErrSyntax},
		{name: "bare plus", in: "+", wantErr: ErrSyntax},
		{name: "bare point", in: ".", wantErr: ErrSyntax},
		{name: "signed bare point", in: "-.", wantErr: ErrSyntax},
		{name: "missing integer part", in: ".5", wantErr: ErrSyntax},
		{name: "trailing decimal point", in: "5.", wantErr: ErrSyntax},
		{name: "double decimal point", in: "1.2.3", wantErr: ErrSyntax},
		{name: "double sign", in: "--1", wantErr: ErrSyntax},
		{name: "trailing sign", in: "1-", wantErr: ErrSyntax},
		{name: "sign after point", in: "1.-2", wantErr: ErrSyntax},

		{name: "leading whitespace", in: " 1", wantErr: ErrSyntax},
		{name: "trailing whitespace", in: "1 ", wantErr: ErrSyntax},
		{name: "embedded whitespace", in: "1 000", wantErr: ErrSyntax},
		{name: "tab", in: "\t1", wantErr: ErrSyntax},
		{name: "newline", in: "1\n", wantErr: ErrSyntax},

		{name: "comma grouping", in: "1,000.00", wantErr: ErrSyntax},
		{name: "underscore grouping", in: "1_000", wantErr: ErrSyntax},
		{name: "apostrophe grouping", in: "1'000", wantErr: ErrSyntax},
		{name: "currency symbol", in: "$1.00", wantErr: ErrSyntax},
		{name: "trailing currency code", in: "1.00USD", wantErr: ErrSyntax},
		{name: "percent suffix", in: "5%", wantErr: ErrSyntax},

		{name: "lowercase exponent", in: "1e5", wantErr: ErrSyntax},
		{name: "uppercase exponent", in: "1E5", wantErr: ErrSyntax},
		{name: "negative exponent", in: "1.5e-3", wantErr: ErrSyntax},

		{name: "hexadecimal", in: "0x10", wantErr: ErrSyntax},
		{name: "letters", in: "abc", wantErr: ErrSyntax},
		{name: "not a number", in: "NaN", wantErr: ErrSyntax},
		{name: "infinity", in: "Inf", wantErr: ErrSyntax},
		{name: "non-ascii digits", in: "١٢٣", wantErr: ErrSyntax},

		{name: "nine significant decimal places", in: "1.123456789", wantErr: ErrPrecision},
		{name: "significant digit far beyond the scale", in: "0.0000000000000001", wantErr: ErrPrecision},
		{name: "trailing zeros then a significant digit", in: "1.000000000001", wantErr: ErrPrecision},

		{name: "one quantum above the maximum", in: "92233720368.54775808", wantErr: ErrRange},
		{name: "one quantum below the minimum", in: "-92233720368.54775809", wantErr: ErrRange},
		{name: "far above the maximum", in: "99999999999", wantErr: ErrRange},
		{name: "absurdly large", in: "999999999999999999999999999999", wantErr: ErrRange},
		{name: "absurdly small", in: "-999999999999999999999999999999", wantErr: ErrRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, got, "rejected input must not return a partial value")
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{name: "zero", in: 0, want: "0"},
		{name: "whole number", in: scale, want: "1"},
		{name: "negative whole number", in: -scale, want: "-1"},
		{name: "trailing zeros are removed", in: 12_345_000_000, want: "123.45"},
		{name: "all eight places are kept when significant", in: 112_345_678, want: "1.12345678"},
		{name: "one quantum", in: 1, want: "0.00000001"},
		{name: "one negative quantum", in: -1, want: "-0.00000001"},
		{name: "leading fraction zeros are kept", in: 50_000_000, want: "0.5"},
		{name: "interior zeros are kept", in: 100_000_001, want: "1.00000001"},
		{name: "five decimal places", in: 108_473_000, want: "1.08473"},
		{name: "maximum", in: maxRaw, want: "92233720368.54775807"},
		{name: "minimum", in: minRaw, want: "-92233720368.54775808"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Format(tt.in))
		})
	}
}

// TestFormatIsCanonical states the canonical-output rules from ADR-004 as
// assertions over the whole representable spread, so a future change to
// Format cannot quietly reintroduce scientific notation or padding.
func TestFormatIsCanonical(t *testing.T) {
	values := []int64{
		minRaw, minRaw + 1, -scale, -1, 0, 1, scale, 12_345_000_000, maxRaw,
	}

	for _, v := range values {
		out := Format(v)

		assert.NotContains(t, out, "e", "no scientific notation: %q", out)
		assert.NotContains(t, out, "E", "no scientific notation: %q", out)
		assert.NotContains(t, out, ",", "no grouping separators: %q", out)
		assert.NotContains(t, out, " ", "no whitespace: %q", out)
		assert.NotEqual(t, byte('.'), out[len(out)-1], "no trailing decimal point: %q", out)
		assert.NotEqual(t, "-0", out, "negative zero is normalized: %q", out)

		if i := indexByte(out, '.'); i >= 0 {
			assert.NotEqual(t, byte('0'), out[len(out)-1],
				"no unnecessary trailing zeros: %q", out)
			assert.LessOrEqual(t, len(out)-i-1, places,
				"never more than the scale's places: %q", out)
		}
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	values := []int64{
		minRaw, minRaw + 1, -12_345_000_000, -scale, -1, 0, 1,
		9, 50_000_000, scale, 108_473_000, 112_345_678, 12_345_000_000,
		maxRaw - 1, maxRaw,
	}

	for _, v := range values {
		text := Format(v)
		got, err := Parse(text)
		require.NoErrorf(t, err, "Format produced text Parse rejects: %q", text)
		assert.Equalf(t, v, got, "round trip changed the value through %q", text)
	}
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
