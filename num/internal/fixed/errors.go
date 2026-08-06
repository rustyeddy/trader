package fixed

import "errors"

// Representation-level errors.
//
// These describe failures of the exact scaled-integer representation itself.
// The num package wraps or re-exports them where a caller needs to distinguish
// causes with errors.Is; it does not expose this package's identity.
var (
	// ErrOverflow reports a result too large for the scaled representation.
	ErrOverflow = errors.New("value overflows exact representation")

	// ErrUnderflow reports a result too small (too negative) for the scaled
	// representation.
	ErrUnderflow = errors.New("value underflows exact representation")

	// ErrDivideByZero reports division by an exact zero.
	ErrDivideByZero = errors.New("division by zero")

	// ErrNotRepresentable reports an operation whose result cannot be
	// represented even though it is within range.  Negation and absolute value
	// of MinRaw are the canonical cases: the positive counterpart of MinInt64
	// does not exist in signed int64.
	ErrNotRepresentable = errors.New("result is not representable")

	// ErrSyntax reports decimal text that is not a well-formed plain decimal
	// number: empty input, stray signs, missing digits, embedded whitespace,
	// grouping separators, exponent notation, or any other malformed form.
	ErrSyntax = errors.New("invalid decimal syntax")

	// ErrPrecision reports decimal text carrying more than Places decimal
	// places.  Exact parsing never silently rounds; excess precision is an
	// error the caller must resolve.
	ErrPrecision = errors.New("excess decimal precision")

	// ErrRange reports decimal text whose value lies outside the representable
	// scaled range.
	ErrRange = errors.New("value out of range")
)
