package account

import "errors"

// ErrInvalidSnapshot reports a SnapshotParams value that fails
// NewSnapshot's validation.
var ErrInvalidSnapshot = errors.New("account: invalid snapshot")

// ErrInvalidReference reports a Reference value that fails
// NewReference's validation.
var ErrInvalidReference = errors.New("account: invalid reference")
