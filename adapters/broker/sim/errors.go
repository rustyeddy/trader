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

// ErrInvalidObservation reports an Observation that fails Advance's
// validation.
var ErrInvalidObservation = errors.New("sim: invalid observation")

// ErrAmbiguousIntrabarOrder reports that an Observation passed to
// Advance would trigger more than one of an account's pending orders
// for the same listing within one bar, under IntrabarRejectAmbiguous
// (ADR-026). OHLC data alone cannot establish which order's trigger
// the market actually reached first, and — since a second fill against
// a listing that already holds a Position is itself rejected
// (ErrPositionUpdateUnsupported) — which order goes first changes the
// resulting account state. None of the conflicting account's orders
// for that listing are filled; every one remains exactly as it was.
var ErrAmbiguousIntrabarOrder = errors.New("sim: observation would trigger more than one pending order for this listing within one bar")
