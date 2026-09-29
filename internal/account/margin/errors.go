package margin

import "errors"

var (
	// ErrInvalidPolicy reports a margin Policy that cannot compute a
	// requirement: a nil Policy, or a Ratio that is not positive.
	ErrInvalidPolicy = errors.New("margin: invalid policy")

	// ErrMissingMark reports an open position whose instrument has no
	// valuation price in the supplied Marks. A missing mark is never
	// treated as zero, and never replaced by the position's AvgPrice.
	ErrMissingMark = errors.New("margin: missing mark")

	// ErrCurrencyMismatch reports a listing whose settlement currency
	// differs from the account currency. v1 performs no FX conversion.
	ErrCurrencyMismatch = errors.New("margin: currency mismatch")

	// ErrInvalidInput reports structurally unusable input, such as an
	// unconstructed listing or more than one open position in the
	// instrument a Change describes.
	ErrInvalidInput = errors.New("margin: invalid input")
)
