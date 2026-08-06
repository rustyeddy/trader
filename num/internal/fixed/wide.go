package fixed

import "math/bits"

// Widened multiplication and division of scaled values.
//
// Two operands at the common scale cannot be multiplied directly: the raw
// product (a*S)*(b*S) carries the scale twice and, for realistic market
// values, exceeds int64 long before the true result does.  ADR-004 therefore
// requires a widened signed intermediate followed by scale restoration:
//
//	result = round((a * b) / S)
//
// Division is the same primitive with the operands rearranged:
//
//	result = round((a * S) / b)
//
// Both reduce to one exact 128-bit multiply-then-divide.  There is no float64
// escape path: a product that cannot be restored to the common scale is an
// error, never an approximation.

// MulScaled returns the product of two raw scaled values, restored to the
// common scale and rounded by mode.
//
// An unrecognized mode reports ErrInvalidRounding rather than silently
// falling back to a default policy.
func MulScaled(a, b int64, mode Rounding) (int64, error) {
	if !mode.valid() {
		return 0, ErrInvalidRounding
	}
	return mulDiv(a, b, scale, mode)
}

// DivScaled returns the quotient of two raw scaled values, restored to the
// common scale and rounded by mode.
//
// A zero divisor reports ErrDivideByZero. An unrecognized mode reports
// ErrInvalidRounding rather than silently falling back to a default policy.
func DivScaled(a, b int64, mode Rounding) (int64, error) {
	if !mode.valid() {
		return 0, ErrInvalidRounding
	}
	if b == 0 {
		return 0, ErrDivideByZero
	}
	return mulDiv(a, scale, b, mode)
}

// mulDiv computes round(a*b/c) exactly, using a 128-bit intermediate product.
//
// Signs are stripped first so the 128-bit work operates on magnitudes only.
// Handling the sign in one place keeps rounding policies from having to reason
// about negative remainders, which is where truncating integer division
// usually goes wrong.
func mulDiv(a, b, c int64, mode Rounding) (int64, error) {
	if c == 0 {
		return 0, ErrDivideByZero
	}

	negative := (a < 0) != (b < 0) != (c < 0)
	if a == 0 || b == 0 {
		return 0, nil
	}

	hi, lo := bits.Mul64(magnitude(a), magnitude(b))
	div := magnitude(c)

	// 128-by-64 division in two steps.  The first step's remainder is strictly
	// less than div, which is exactly the precondition bits.Div64 requires of
	// its high word, so neither call can panic.
	quoHi, rem := bits.Div64(0, hi, div)
	quoLo, rem := bits.Div64(rem, lo, div)

	// A quotient needing more than 64 bits cannot be narrowed to int64 under
	// any rounding policy.
	if quoHi != 0 {
		return 0, overflowFor(negative)
	}

	if roundMagnitude(quoLo, rem, div, negative, mode) {
		quoLo++
		if quoLo == 0 { // wrapped past the top of uint64
			return 0, overflowFor(negative)
		}
	}

	return fromMagnitude(quoLo, negative)
}

// magnitude returns the absolute value of v as an unsigned integer.
//
// Unsigned negation is used rather than -v so that minRaw, whose positive
// counterpart is not a valid int64, yields its true magnitude of 2^63.
func magnitude(v int64) uint64 {
	u := uint64(v)
	if v < 0 {
		u = -u
	}
	return u
}

// fromMagnitude converts a magnitude and sign back to a raw scaled value,
// reporting overflow or underflow when the signed range cannot hold it.
//
// The negative range extends one further than the positive range, so the two
// signs are bounded separately.
func fromMagnitude(mag uint64, negative bool) (int64, error) {
	const negativeLimit = uint64(1) << 63 // magnitude of minRaw

	if negative {
		switch {
		case mag > negativeLimit:
			return 0, ErrUnderflow
		case mag == negativeLimit:
			return minRaw, nil
		default:
			return -int64(mag), nil
		}
	}

	if mag > uint64(maxRaw) {
		return 0, ErrOverflow
	}
	return int64(mag), nil
}

// overflowFor reports the range error matching the sign of an out-of-range
// result.
func overflowFor(negative bool) error {
	if negative {
		return ErrUnderflow
	}
	return ErrOverflow
}
