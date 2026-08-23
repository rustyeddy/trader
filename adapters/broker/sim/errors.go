package sim

import "errors"

// ErrInvalidConfig reports a Deps or AccountConfig value that fails
// NewBroker's validation.
var ErrInvalidConfig = errors.New("sim: invalid config")
