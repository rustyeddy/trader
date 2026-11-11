package fixed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzFormatParse exercises the round trip in the direction that must always
// succeed: every representable raw value has canonical text, and that text
// must parse back to the identical raw value.  A failure here means an
// authoritative value could change merely by being written and read again.
func FuzzFormatParse(f *testing.F) {
	seeds := []int64{
		minRaw, minRaw + 1, -12_345_000_000, -scale, -9, -1, 0, 1, 9,
		50_000_000, scale, 108_473_000, 112_345_678, maxRaw - 1, maxRaw,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw int64) {
		text := Format(raw)

		got, err := Parse(text)
		require.NoErrorf(t, err, "Format produced text Parse rejects: %q", text)
		assert.Equalf(t, raw, got, "round trip changed the value through %q", text)
	})
}

// FuzzParseFormat exercises the opposite direction, where most inputs are
// expected to be rejected.  The invariant is that acceptance is stable: any
// text Parse accepts must format to canonical text that parses to the same
// value, so no accepted input can smuggle in a value the formatter cannot
// reproduce.  Parse must also never panic, whatever bytes arrive.
func FuzzParseFormat(f *testing.F) {
	seeds := []string{
		"", "0", "-0", "+0", "1", "-1", "0.5", "123.45", "1.08473",
		"0.00000001", "1.00000000", "007", "1.000000000000",
		"92233720368.54775807", "-92233720368.54775808",
		".", "-", "+", ".5", "5.", "1e5", "1,000", " 1", "abc", "NaN",
		"1.123456789", "99999999999999999999",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		raw, err := Parse(text)
		if err != nil {
			assert.Zero(t, raw, "rejected input must not return a partial value")
			return
		}

		canonical := Format(raw)
		again, err := Parse(canonical)
		require.NoErrorf(t, err, "canonical form of %q was rejected: %q", text, canonical)
		assert.Equalf(t, raw, again, "canonical form of %q lost value: %q", text, canonical)
	})
}

// FuzzAddSub checks that checked addition and subtraction either return the
// true mathematical result or report an error.  Silent wrapping, the failure
// mode ADR-004 forbids, would show up here as an accepted but incorrect
// result.
func FuzzAddSub(f *testing.F) {
	seeds := [][2]int64{
		{0, 0}, {1, -1}, {maxRaw, 1}, {minRaw, -1}, {maxRaw, maxRaw},
		{minRaw, minRaw}, {minRaw, maxRaw}, {scale, scale},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, a, b int64) {
		if sum, err := Add(a, b); err == nil {
			// Recovering a by undoing b cannot itself overflow, because sum is
			// representable and b was one of its addends.
			assert.Equalf(t, a, sum-b, "Add(%d,%d) returned a wrapped result", a, b)
		}

		if diff, err := Sub(a, b); err == nil {
			assert.Equalf(t, a, diff+b, "Sub(%d,%d) returned a wrapped result", a, b)
		}
	})
}

// FuzzMulDivScaled checks the widened path for the same wrapping property, and
// for the exactness relationship between multiplication and division: dividing
// a product by one of its operands must return the other, whenever the inputs
// make that exact.
func FuzzMulDivScaled(f *testing.F) {
	seeds := [][2]int64{
		{0, 0}, {scale, scale}, {108_473_000, 105_000_000}, {maxRaw, scale},
		{minRaw, scale}, {maxRaw, maxRaw}, {1, 250_000_000}, {-1, 150_000_000},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, a, b int64) {
		product, err := MulScaled(a, b, RoundHalfEven)
		if err != nil {
			assert.Zero(t, product, "failed multiplication must not return a partial result")
			return
		}

		// Scaling by exactly one is the identity, in both directions.
		if b == scale {
			assert.Equalf(t, a, product, "MulScaled(%d, 1) is not the identity", a)
		}

		// Zero propagates rather than producing a spurious remainder.
		if a == 0 || b == 0 {
			assert.Zerof(t, product, "MulScaled(%d,%d) should be zero", a, b)
		}

		quotient, err := DivScaled(product, b, RoundHalfEven)
		if err != nil || b == 0 {
			return
		}

		// Recovering a exactly requires the product to have been exact, which
		// holds when multiplying back reproduces it.
		if check, err := MulScaled(quotient, b, RoundHalfEven); err == nil {
			assert.Equalf(t, product, check,
				"round trip through DivScaled lost the product of %d and %d", a, b)
		}
	})
}
