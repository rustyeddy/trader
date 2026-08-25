package sim

import "errors"

// ErrInvalidConfig reports a Deps or AccountConfig value that fails
// NewBroker's validation.
var ErrInvalidConfig = errors.New("sim: invalid config")

// ErrPositionUpdateUnsupported reports a market order fill against a
// listing where the account already holds a non-flat Position.
// Correctly adding to, reducing, closing, or reversing an existing
// position requires weighted-average cost basis and realized PnL
// accounting that issue #152 (M3-09) owns; this package intentionally
// returns this explicit, classifiable error rather than silently
// computing an incorrect average price or PnL. Only the simplest case
// — opening a new position from flat — is implemented here (issue
// #149, M3-06). See the package doc comment.
var ErrPositionUpdateUnsupported = errors.New("sim: updating an existing position is not yet supported")
