package num

import (
	"errors"

	"github.com/rustyeddy/trader/num/internal/fixed"
)

// Errors returned by num's semantic operations.
//
// num/internal/fixed's representation errors (overflow, underflow, divide by
// zero, syntax, precision, range) are not re-exported by identity; num wraps
// them so callers can use errors.Is against the sentinels below without ever
// importing num/internal/fixed, which packages outside num are forbidden to
// do.
var (
	// ErrOverflow reports a result too large for the exact representation.
	ErrOverflow = errors.New("num: value overflows exact representation")

	// ErrUnderflow reports a result too small (too negative) for the exact
	// representation.
	ErrUnderflow = errors.New("num: value underflows exact representation")

	// ErrDivideByZero reports division by an exact zero.
	ErrDivideByZero = errors.New("num: division by zero")

	// ErrNotRepresentable reports an operation, such as negating the most
	// negative representable value, whose result has no representation.
	ErrNotRepresentable = errors.New("num: result is not representable")

	// ErrSyntax reports decimal text that is not a well-formed plain decimal
	// number.
	ErrSyntax = errors.New("num: invalid decimal syntax")

	// ErrPrecision reports decimal text carrying more than eight decimal
	// places of significant precision.
	ErrPrecision = errors.New("num: excess decimal precision")

	// ErrRange reports decimal text or a scaled value outside the
	// representable range.
	ErrRange = errors.New("num: value out of range")

	// ErrNegative reports a negative value where the semantic type requires a
	// non-negative one, such as Price or Quantity.
	ErrNegative = errors.New("num: negative value not permitted")

	// ErrCurrencyMismatch reports an operation attempted between Money values
	// denominated in different currencies.
	ErrCurrencyMismatch = errors.New("num: currency mismatch")

	// ErrInvalidCurrency reports a currency code that fails structural
	// validation: it must be 3 to 5 uppercase ASCII letters.
	ErrInvalidCurrency = errors.New("num: invalid currency code")

	// ErrMissingCurrency reports a Money value or operation missing its
	// required currency, including the Go zero value of Money.
	ErrMissingCurrency = errors.New("num: missing currency")
)

// wrapFixedErr translates a num/internal/fixed representation error into its
// num sentinel, so num is the only package that ever names the fixed package.
// Non-representation errors, including nil, pass through unchanged.
func wrapFixedErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fixed.ErrOverflow):
		return ErrOverflow
	case errors.Is(err, fixed.ErrUnderflow):
		return ErrUnderflow
	case errors.Is(err, fixed.ErrDivideByZero):
		return ErrDivideByZero
	case errors.Is(err, fixed.ErrNotRepresentable):
		return ErrNotRepresentable
	case errors.Is(err, fixed.ErrSyntax):
		return ErrSyntax
	case errors.Is(err, fixed.ErrPrecision):
		return ErrPrecision
	case errors.Is(err, fixed.ErrRange):
		return ErrRange
	default:
		return err
	}
}
