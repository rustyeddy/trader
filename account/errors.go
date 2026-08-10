package account

import "errors"

// ErrInvalidSnapshot reports a SnapshotParams value that fails
// NewSnapshot's validation.
var ErrInvalidSnapshot = errors.New("account: invalid snapshot")
