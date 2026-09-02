package indicator

import "errors"

// ErrInvalidPeriod marks a NewEMA call whose period is not positive.
var ErrInvalidPeriod = errors.New("indicator: period must be positive")
