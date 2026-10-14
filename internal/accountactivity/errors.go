package accountactivity

import "errors"

// ErrInvalidActivity reports an Activity that fails Validate. The wrapped
// message names the field and the rule, never a monetary value.
var ErrInvalidActivity = errors.New("accountactivity: invalid activity")

// ErrInvalidValue reports a Value constructed without the source its basis
// requires.
var ErrInvalidValue = errors.New("accountactivity: invalid value")
