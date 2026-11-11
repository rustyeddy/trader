package fixed

import "errors"

// Representation-level errors.
//
// These describe failures of the exact scaled-integer representation itself.
// The num package re-exports the same values under its own names (num.ErrOverflow
// is fixed.ErrOverflow), so callers can use errors.Is against num's sentinels
// without importing this package. Their messages therefore carry num's "num: "
// prefix: that text is part of num's public error contract.
var (
	// ErrOverflow reports a result too large for the scaled representation.
	ErrOverflow = errors.New("num: value overflows exact representation")

	// ErrUnderflow reports a result too small (too negative) for the scaled
	// representation.
	ErrUnderflow = errors.New("num: value underflows exact representation")

	// ErrDivideByZero reports division by an exact zero.
	ErrDivideByZero = errors.New("num: division by zero")

	// ErrNotRepresentable reports an operation whose result cannot be
	// represented even though it is within range.  Negation and absolute value
	// of minRaw are the canonical cases: the positive counterpart of MinInt64
	// does not exist in signed int64.
	ErrNotRepresentable = errors.New("num: result is not representable")

	// ErrSyntax reports decimal text that is not a well-formed plain decimal
	// number: empty input, stray signs, missing digits, embedded whitespace,
	// grouping separators, exponent notation, or any other malformed form.
	ErrSyntax = errors.New("num: invalid decimal syntax")

	// ErrPrecision reports decimal text carrying more significant fraction
	// digits than the places constant supports.  Exact parsing never
	// silently rounds; excess precision is an error the caller must resolve.
	ErrPrecision = errors.New("num: excess decimal precision")

	// ErrRange reports decimal text whose value lies outside the representable
	// scaled range.
	ErrRange = errors.New("num: value out of range")

	// ErrInvalidRounding reports a Rounding value that is none of the four
	// named policies.  An unrecognized mode is rejected outright rather than
	// silently resolved to a default: ADR-004 forbids exactly this kind of
	// hidden policy fallback.
	ErrInvalidRounding = errors.New("invalid rounding mode")
)
