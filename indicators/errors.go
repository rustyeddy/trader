package indicators

import "errors"

// ErrInvalidPeriod marks a constructor call whose period is not positive (or, for ATR, too large).
var ErrInvalidPeriod = errors.New("indicators: period must be positive")

// ErrNonFiniteSample marks an Update call whose sample is NaN or
// infinite. Update rejects it outright rather than silently poisoning
// the EMA's accumulated state — see EMA.Update's own doc comment.
var ErrNonFiniteSample = errors.New("indicators: sample must be finite")
