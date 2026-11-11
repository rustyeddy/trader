package num

import (
	"errors"

	"github.com/rustyeddy/trader/num/internal/fixed"
)

// Errors returned by num's semantic operations.
//
// The first seven are num/internal/fixed's representation errors, re-exported
// by identity: errors.Is(err, num.ErrOverflow) holds for an overflow in the
// representation layer, and callers never import num/internal/fixed, which
// packages outside num are forbidden to do. The rest are semantic errors that
// only num raises.
var (
	// ErrOverflow reports a result too large for the exact representation.
	ErrOverflow = fixed.ErrOverflow

	// ErrUnderflow reports a result too small (too negative) for the exact
	// representation.
	ErrUnderflow = fixed.ErrUnderflow

	// ErrDivideByZero reports division by an exact zero.
	ErrDivideByZero = fixed.ErrDivideByZero

	// ErrNotRepresentable reports an operation, such as negating the most
	// negative representable value, whose result has no representation.
	ErrNotRepresentable = fixed.ErrNotRepresentable

	// ErrSyntax reports decimal text that is not a well-formed plain decimal
	// number.
	ErrSyntax = fixed.ErrSyntax

	// ErrPrecision reports decimal text carrying more than eight decimal
	// places of significant precision.
	ErrPrecision = fixed.ErrPrecision

	// ErrRange reports decimal text or a scaled value outside the
	// representable range.
	ErrRange = fixed.ErrRange

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
