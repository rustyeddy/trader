package indicator

import "errors"

// ErrInvalidPeriod marks a NewEMA call whose period is not positive.
var ErrInvalidPeriod = errors.New("indicator: period must be positive")

// ErrNonFiniteSample marks an Update call whose sample is NaN or
// infinite. Update rejects it outright rather than silently poisoning
// the EMA's accumulated state — see EMA.Update's own doc comment.
var ErrNonFiniteSample = errors.New("indicator: sample must be finite")
