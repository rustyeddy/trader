package fixed

// Rounding selects how a result that is not exactly representable at the
// common scale is resolved.
//
// ADR-004 names four supported policies and makes nearest/ties-to-even the
// default.  All four are implemented here; the num package currently exposes
// only the default, because no domain operation has yet required a
// caller-selected policy.  Implicit integer truncation is not one of these
// policies: [RoundTowardZero] must be chosen deliberately.
type Rounding uint8

const (
	// RoundHalfEven rounds to the nearest representable value, resolving exact
	// midpoints toward the even neighbour.  This is the ADR-004 default.
	RoundHalfEven Rounding = iota

	// RoundTowardZero discards the remainder, moving toward zero for both
	// signs.
	RoundTowardZero

	// RoundDown rounds toward negative infinity.
	RoundDown

	// RoundUp rounds toward positive infinity.
	RoundUp
)

// String returns the policy name, for error messages and test output.
func (r Rounding) String() string {
	switch r {
	case RoundHalfEven:
		return "half-even"
	case RoundTowardZero:
		return "toward-zero"
	case RoundDown:
		return "down"
	case RoundUp:
		return "up"
	default:
		return "unknown"
	}
}

// roundMagnitude reports whether the magnitude of a quotient should be
// incremented, given the truncated magnitude quotient, the magnitude
// remainder, the divisor magnitude, and the sign of the true result.
//
// All three numeric inputs are magnitudes, so the caller's sign handling stays
// in one place instead of being threaded through every comparison.  The
// remainder is strictly less than the divisor, and the divisor is at most
// 2^63, so doubling the remainder to compare against the divisor cannot
// overflow uint64.
func roundMagnitude(quo, rem, div uint64, negative bool, mode Rounding) bool {
	if rem == 0 {
		return false
	}

	switch mode {
	case RoundTowardZero:
		return false

	case RoundDown:
		return negative

	case RoundUp:
		return !negative

	default: // RoundHalfEven
		twice := rem * 2
		switch {
		case twice > div:
			return true
		case twice < div:
			return false
		default:
			// Exact midpoint: move to the even neighbour.
			return quo%2 == 1
		}
	}
}
